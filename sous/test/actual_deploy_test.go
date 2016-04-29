package test

import (
	"bytes"
	"crypto/md5"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"text/template"

	"github.com/opentable/singularity"
	"github.com/opentable/singularity/dtos"
	"github.com/opentable/sous/sous"
	"github.com/opentable/sous/util/docker_registry"
	"github.com/opentable/test_with_docker"
	"github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
)

var ip, registryName, imageName, singularityUrl string

func TestMain(m *testing.M) {
	os.Exit(wrapCompose(m))
}

func TestGetLabels(t *testing.T) {
	assert := assert.New(t)
	cl := docker_registry.NewClient()
	cl.BecomeFoolishlyTrusting()

	labels, err := cl.LabelsForImageName(imageName)

	assert.Nil(err)
	assert.Contains(labels, sous.DockerRepoLabel)
}

func TestGetRunningDeploymentSet(t *testing.T) {
	assert := assert.New(t)

	deps, err := sous.GetRunningDeploymentSet([]string{singularityUrl})
	assert.Nil(err)
	assert.Equal(3, len(deps))
	var grafana *sous.Deployment
	for i := range deps {
		if deps[i].SourceVersion.RepoURL == "https://github.com/opentable/docker-grafana.git" {
			grafana = &deps[i]
		}
	}
	assert.NotNil(grafana)
	assert.Equal(singularityUrl, grafana.Cluster)
	assert.Regexp("^0\\.1", grafana.Resources["cpus"])    // XXX strings and floats...
	assert.Regexp("^100\\.", grafana.Resources["memory"]) // XXX strings and floats...
	assert.Equal("1", grafana.Resources["ports"])         // XXX strings and floats...
	assert.Equal(17, grafana.Patch)
	assert.Equal("91495f1b1630084e301241100ecf2e775f6b672c", grafana.Meta)
	assert.Equal(1, grafana.NumInstances)
	assert.Equal(sous.ManifestKindService, grafana.Kind)
}

func wrapCompose(m *testing.M) (resultCode int) {
	log.SetFlags(log.Flags() | log.Lshortfile)
	defer func() {
		log.Println("Cleaning up...")
		if err := recover(); err != nil {
			log.Print("Panic: ", err)
			resultCode = 1
		}
	}()

	machineName := "default"
	ip, err := test_with_docker.MachineIP(machineName)
	if err != nil {
		panic(err)
	}

	composeDir := "test-registry"
	registryCertName := "testing.crt"
	registryName = fmt.Sprintf("%s:%d", ip, 5000)

	err = registryCerts(composeDir, registryCertName, machineName, ip)
	if err != nil {
		panic(fmt.Errorf("building registry certs failed: %s", err))
	}

	_, started, err := test_with_docker.ComposeServices("default", composeDir, map[string]uint{"Singularity": 7099, "Registry": 5000})
	defer test_with_docker.Shutdown(started)

	registerAndDeploy(ip, "hello-labels", "hello-labels", []int32{})
	registerAndDeploy(ip, "hello-server-labels", "hello-server-labels", []int32{80})
	registerAndDeploy(ip, "grafana-repo", "grafana-labels", []int32{})
	imageName = fmt.Sprintf("%s/%s:%s", registryName, "grafana-repo", "latest")

	log.Println("   *** Beginning tests... ***\n\n")
	resultCode = m.Run()
	return
}

func registerAndDeploy(ip, reponame, dir string, ports []int32) (err error) {
	imageName := fmt.Sprintf("%s/%s:%s", registryName, reponame, "latest")
	err = buildAndPushContainer(dir, imageName)
	if err != nil {
		panic(fmt.Errorf("building test container failed: %s", err))
	}

	singularityUrl = fmt.Sprintf("http://%s:%d/singularity", ip, 7099)
	err = startInstance(singularityUrl, imageName, ports)
	if err != nil {
		panic(fmt.Errorf("starting a singularity instance failed: %s", err))
	}

	return
}

var successfulBuildRE = regexp.MustCompile(`Successfully built (\w+)`)

type dtoMap map[string]interface{}

func loadMap(fielder dtos.Fielder, m dtoMap) dtos.Fielder {
	_, err := dtos.LoadMap(fielder, m)
	if err != nil {
		log.Fatal(err)
	}

	return fielder
}

var notInIdRE = regexp.MustCompile(`[-/]`)

func idify(in string) string {
	return notInIdRE.ReplaceAllString(in, "")
}

func startInstance(url, imageName string, ports []int32) error {
	reqId := idify(imageName)

	sing := singularity.NewClient(url)

	req := loadMap(&dtos.SingularityRequest{}, map[string]interface{}{
		"Id":          reqId,
		"RequestType": dtos.SingularityRequestRequestTypeSERVICE,
		"Instances":   int32(1),
	}).(*dtos.SingularityRequest)

	_, err := sing.PostRequest(req)
	if err != nil {
		return err
	}

	dockerInfo := loadMap(&dtos.SingularityDockerInfo{}, dtoMap{
		"Image": imageName,
	}).(*dtos.SingularityDockerInfo)

	depReq := loadMap(&dtos.SingularityDeployRequest{}, dtoMap{
		"Deploy": loadMap(&dtos.SingularityDeploy{}, dtoMap{
			"Id":        idify(uuid.NewV4().String()),
			"RequestId": reqId,
			"Resources": loadMap(&dtos.Resources{}, dtoMap{
				"Cpus":     0.1,
				"MemoryMb": 100.0,
				"NumPorts": int32(1),
			}),
			"ContainerInfo": loadMap(&dtos.SingularityContainerInfo{}, dtoMap{
				"Type":   dtos.SingularityContainerInfoSingularityContainerTypeDOCKER,
				"Docker": dockerInfo,
			}),
		}),
	}).(*dtos.SingularityDeployRequest)

	_, err = sing.Deploy(depReq)
	if err != nil {
		return err
	}

	return nil
}

func buildAndPushContainer(containerDir, tagName string) error {
	build := exec.Command("docker", "build", ".")
	build.Dir = containerDir
	output, err := build.CombinedOutput()
	if err != nil {
		return err
	}

	match := successfulBuildRE.FindStringSubmatch(string(output))
	if match == nil {
		return fmt.Errorf("Couldn't find container id in:\n%s", output)
	}

	containerId := match[1]
	tag := exec.Command("docker", "tag", containerId, tagName)
	tag.Dir = containerDir
	output, err = tag.CombinedOutput()
	if err != nil {
		return err
	}

	push := exec.Command("docker", "push", tagName)
	push.Dir = containerDir
	output, err = push.CombinedOutput()
	if err != nil {
		return err
	}

	return nil
}

// registryCerts makes sure that we'll be able to reach the test registry
// Find the docker-machine IP
// Get the SAN from the existing test cert
//   If different, template out a new openssl config
//   Regenerate the key/cert pair
//   docker-compose rebuild the registry service
func registryCerts(composeDir, registryCertName, machineName, ipstr string) error {
	ip := net.ParseIP(ipstr)
	certPath := filepath.Join(composeDir, registryCertName)
	caPath := fmt.Sprintf("/etc/docker/certs.d/%s/ca.crt", registryName)

	certIPs, err := getCertIPSans(filepath.Join(composeDir, registryCertName))
	if err != nil {
		return err
	}

	haveIP := false

	for _, certIP := range certIPs {
		if certIP.Equal(ip) {
			haveIP = true
			break
		}
	}

	if !haveIP {
		log.Print("Rebuilding the registry certificate")
		certIPs = append(certIPs, ip)
		err = buildTestingKeypair(composeDir, certIPs)
		if err != nil {
			return err
		}

		err = test_with_docker.RebuildService(machineName, composeDir, "registry")
		if err != nil {
			return err
		}
	}

	certFile, err := os.Open(filepath.Join(composeDir, "testing.crt"))
	if err != nil {
		return err
	}

	hash := md5.New()
	io.Copy(hash, certFile)
	caHash := fmt.Sprintf("%x", hash.Sum(nil))
	md5s, err := test_with_docker.MachineMD5s(machineName, caPath)
	if err != nil {
		return err
	}

	remoteHash, present := md5s[caPath]
	if !present {
		return fmt.Errorf("Could not determine hash of CA on machine at %s", caPath)
	}

	if strings.Compare(remoteHash, caHash) != 0 {
		log.Printf("Remote and local registry certs differ (%q vs %q)", remoteHash, caHash)
		err = test_with_docker.InstallMachineFile(machineName, certPath, caPath)

		//err = installCA(machineName, registryName, composeDir, registryCertName)
		if err != nil {
			return fmt.Errorf("installCA failed: %s", err)
		}

		err = test_with_docker.SshSudo(machineName, "/etc/init.d/docker", "restart")
		if err != nil {
			return fmt.Errorf("restarting docker machine's daemon failed: %s", err)
		}
	}
	return err
}

func buildTestingKeypair(dir string, certIPs []net.IP) (err error) {
	log.Print(certIPs)
	selfSignConf := "self-signed.conf"
	temp := template.Must(template.New("req").Parse(`
{{- "" -}}
[ req ]
prompt = no
distinguished_name=req_distinguished_name
x509_extensions = va_c3
encrypt_key = no
default_keyfile=testing.key
default_md = sha256

[ va_c3 ]
basicConstraints=critical,CA:true,pathlen:1
{{range . -}}
subjectAltName = IP:{{.}}
{{end}}
[ req_distinguished_name ]
CN=registry.test
{{"" -}}
		`))
	confPath := filepath.Join(dir, selfSignConf)
	reqFile, err := os.Create(confPath)
	if err != nil {
		return
	}
	err = temp.Execute(reqFile, certIPs)
	if err != nil {
		return
	}

	// This is the openssl command to produce a (very weak) self-signed keypair based on the conf templated above.
	// Ultimately, this provides the bare minimum to use the docker registry on a "remote" server
	openssl := exec.Command("openssl", "req", "-newkey", "rsa:512", "-x509", "-days", "365", "-out", "testing.crt", "-config", selfSignConf, "-batch")
	openssl.Dir = dir
	_, err = openssl.CombinedOutput()

	return
}

func getCertIPSans(certPath string) ([]net.IP, error) {
	certFile, err := os.Open(certPath)
	if _, ok := err.(*os.PathError); ok {
		return make([]net.IP, 0), nil
	}
	if err != nil {
		return nil, err
	}

	certBuf := bytes.Buffer{}
	_, err = certBuf.ReadFrom(certFile)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(certBuf.Bytes())

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}

	return cert.IPAddresses, nil
}