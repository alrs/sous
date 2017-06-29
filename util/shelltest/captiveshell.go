package shelltest

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

const (
	headerMarker = `# END SCRIPT HEADER`
	footerMarker = `# BEGIN SCRIPT FOOTER`

	stateCapture = `
trap '(lasterr=$?; exec >&5; echo -n $lasterr; history 1)' ERR
` + headerMarker
	exitCapture = footerMarker + `
status=$?
env | sort >&4
echo $status >&3
`
)

type (
	captiveShell struct {
		*exec.Cmd
		Stdin io.WriteCloser
		stdout, stderr, blended,
		scriptEnv, scriptErrs *liveStream
		doneRead *bufio.Scanner
		writeDir string
		events   chan int
	}
	// XXX: there's room here for a "stateful stdin" - using DEBUG traps and PS1
	// hacks to keep track of the shell's state, and streaming the stdin in more
	// like a human would - e.g. normal characters very quickly, but with delays
	// after "Enter" (and complete pauses if the "enter" triggers a DEBUG trap).
	// The purpose would be to avoid weird test-level hacks like </dev/null for
	// SSH.
	//
	// One practical upshot is that it wouldn't be 100% either, since it'd have
	// to rely on both timing (i.e. that the shell is likely to trigger the DEBUG
	// trap quickly, but it might not) and fragile environment variables
	// (effectively, we'd need to echo $PS1 every DEBUG and bail out if it'd been
	// changed.)
	//
	// (Or... echo $PS1 and then watch for some version of that on stderr instead
	// of waiting for a PS1 eval hack to write to a stream. Of course, if PS1
	// isn't a simple string (because there's an eval hack...) we can't just
	// match it... so we'd wind up spinning off a shell to evaluate PS1 to find
	// out what it prints... granted that it doesn't screw anything up...
	//
	// ...which is why, for the moment, I'm sticking with </dev/null on SSH.

	liveStream struct {
		name     string
		debugDir string
		pipe     io.Reader
		bufs     []io.Writer
		debugs   []io.Writer
		saveTo   io.Writer
		sync.Mutex
	}

	reseter interface {
		Reset()
	}
)

func newLiveStream(name string, from io.Reader, events <-chan int) *liveStream {
	ls := &liveStream{
		name: name,
		pipe: from,
		bufs: []io.Writer{&bytes.Buffer{}},
	}

	if from != nil {
		go ls.reader(events)
	}
	return ls
}

func newShell(env map[string]string) (sh *captiveShell, err error) {
	sh = &captiveShell{}
	sh.Cmd = exec.Command("bash", "--norc", "-i")

	for k, v := range env {
		sh.Cmd.Env = append(sh.Cmd.Env, k+"="+v)
	}

	sh.events = make(chan int)
	sh.Stdin, err = sh.Cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	sh.blended = newLiveStream("blended", nil, sh.events)

	stdo, err := sh.Cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	sh.stdout = newLiveStream("stdout", stdo, sh.events)
	sh.stdout.addBuf(sh.blended)

	stde, err := sh.Cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	sh.stderr = newLiveStream("stderr", stde, sh.events)
	sh.stderr.addBuf(sh.blended)

	dr, doneWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	sh.doneRead = bufio.NewScanner(dr)

	envR, envW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	sh.scriptEnv = newLiveStream("env", envR, sh.events)

	errR, errW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	sh.scriptErrs = newLiveStream("errors", errR, sh.events)

	sh.Cmd.ExtraFiles = []*os.File{doneWrite, envW, errW}

	sh.Cmd.Start()

	doneWrite.Close()
	envW.Close()
	errW.Close()

	return
}

func (sh *captiveShell) WriteTo(dir string) {
	sh.writeDir = dir
}

func (sh *captiveShell) BlockName(name string) {
	if sh.writeDir == "" {
		return
	}

	path := filepath.Join(sh.writeDir, name)
	os.MkdirAll(path, os.ModePerm)

	sh.stdout.updateSaveTo(path)
	sh.stderr.updateSaveTo(path)
	sh.blended.updateSaveTo(path)
	sh.scriptEnv.updateSaveTo(path)
	sh.scriptErrs.updateSaveTo(path)
}

func (ls *liveStream) updateSaveTo(dir string) {
	ls.Lock()
	defer ls.Unlock()

	path := filepath.Join(dir, ls.name)

	var err error
	ls.saveTo, err = os.Create(path)
	if err != nil {
		panic(err)
	}
}

// Run runs a script in the context of the shell, waits for it to complete and
// returns the Result of running it.
func (sh *captiveShell) Run(script string) (Result, error) {
	st := stateCapture + "\n" + script + "\n" + exitCapture
	sh.Stdin.Write([]byte(st))
	exit, err := sh.readExitStatus()
	if err != nil {
		return Result{}, err
	}

	stdout := sh.stdout.consume(0)
	stderr := sh.stderr.consume(0)
	blended := sh.stdout.consume(1)

	errExits := sh.scriptErrs.consume(0)
	env := sh.scriptEnv.consume(0)

	hre := regexp.MustCompile(`(?ms).*` + headerMarker)
	fre := regexp.MustCompile(`(?ms)` + footerMarker + `.*`)

	stdout = hre.ReplaceAllString(fre.ReplaceAllString(stdout, ""), "")
	blended = hre.ReplaceAllString(fre.ReplaceAllString(blended, ""), "")
	stderr = hre.ReplaceAllString(fre.ReplaceAllString(stderr, ""), "")

	return Result{
		Script:  script,
		Exit:    exit,
		Stdout:  stdout,
		Stderr:  stderr,
		Blended: blended,
		Errs:    errExits,
		Env:     env,
	}, nil
}

func (ls *liveStream) reader(events <-chan int) {
	buf := make([]byte, 1024)
	for {
		select {
		default:
			count, err := ls.pipe.Read(buf)
			if err != nil {
				return
			}
			ls.Write(buf[0:count])
		case <-events:
			return
		}
	}
}

func (ls *liveStream) debugTo(name, prefix string) {
	dir, _ := ioutil.TempDir("", prefix)
	ls.debugDir = dir
	os.MkdirAll(ls.debugDir, os.ModePerm)
	ls.debugs = make([]io.Writer, len(ls.bufs))
	log.Printf("Writing low-level debug output for %s's buffers into %q.", name, ls.debugDir)
	for n := range ls.bufs {
		path := filepath.Join(ls.debugDir, fmt.Sprint(n))
		ls.debugs[n], _ = os.Create(path)
	}
}

func (ls *liveStream) Write(buf []byte) (count int, err error) {
	ls.Lock()
	defer ls.Unlock()

	for n, b := range ls.bufs {
		count, err = b.Write(buf)

		if ls.debugDir != "" {
			_, err := ls.debugs[n].Write(buf)
			if err != nil {
				log.Printf("Error while writing bytes: %v", err)
				log.Printf("Was trying to write: %s", string(buf))
			}
		}
	}

	if ls.saveTo != nil {
		ls.saveTo.Write(buf)
	}

	return
}

func (ls *liveStream) addBuf(buf io.Writer) {
	ls.bufs = append(ls.bufs, buf)
}

func (ls *liveStream) consume(n int) string {
	ls.Lock()
	defer ls.Unlock()
	buf, ok := ls.bufs[n].(fmt.Stringer)
	var str string
	if ok {
		str = buf.String()
	}

	res, ok := ls.bufs[n].(reseter)
	if ok {
		res.Reset()
	}
	return str
}

func (sh *captiveShell) readExitStatus() (int, error) {
	if !sh.doneRead.Scan() {
		return -1, fmt.Errorf("Exit stream closed prematurely!\n%#v\n%#v\n%s\n****\n%s************", sh, sh.Cmd, sh.stdout.consume(0), sh.stderr.consume(0))
	}

	return strconv.Atoi(string(bytes.TrimFunc(sh.doneRead.Bytes(), func(r rune) bool {
		return !strings.Contains(`0123456789`, string(r))
	})))

}
