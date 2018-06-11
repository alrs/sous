package docker

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nyarly/spies"
	sous "github.com/opentable/sous/lib"
	"github.com/opentable/sous/util/docker_registry"
	"github.com/opentable/sous/util/logging"
	"github.com/opentable/sous/util/shell"
	"github.com/pkg/errors"
)

func testSBPDetect(t *testing.T, dockerfile string,
	metadataMap map[string]docker_registry.Metadata) (*sous.DetectResult, error) {
	testDir, err := ioutil.TempDir("testdata", "splitcontainer")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(testDir)

	sh, err := shell.DefaultInDir(testDir)
	if err != nil {
		t.Fatal(err)
	}
	c := &sous.BuildContext{
		Sh:     sh,
		Source: sous.SourceContext{},
	}
	if dockerfile != "" {
		dockerfilePath := filepath.Join(testDir, "Dockerfile")
		if err := ioutil.WriteFile(dockerfilePath, []byte(dockerfile), 0777); err != nil {
			t.Fatal(err)
		}
	}

	rc := docker_registry.NewDummyClient()

	for k, v := range metadataMap {
		rc.AddMetadata(k, v)
	}
	rc.MatchMethod("GetImageMetadata", spies.AnyArgs, docker_registry.Metadata{}, errors.Errorf("no such MD"))
	sbp := NewSplitBuildpack(rc, logging.SilentLogSet())
	dr, err := sbp.Detect(c)

	return dr, err
}

func assertAccepted(t *testing.T, drez *sous.DetectResult, err error) {
	if err != nil {
		t.Errorf("SplitBuildpack reported unexpected error %#v", err)
	}
	if drez == nil {
		t.Errorf("SplitBuildpack returned a nil DetectResult")
	} else if !drez.Compatible {
		t.Errorf("SplitBuildpack incorrectly reported incompatible project: %#v", drez)
	}
}

func assertRejected(t *testing.T, drez *sous.DetectResult, err error) {
	if err != nil {
		return // an error implies rejection
	}
	if drez == nil {
		t.Errorf("SplitBuildpack returned a nil DetectResult")
	} else if drez.Compatible {
		t.Errorf("SplitBuildpack incorrectly reported compatible project: %#v", drez)
	}
}

func assertArgs(t *testing.T, drez *sous.DetectResult, version, revision bool) {
	defer func() {
		if r := recover(); r != nil {
			t.Error(r)
		}
	}()
	if drez.Data.(detectData).HasAppRevisionArg != revision {
		t.Errorf("Expected detected revision arg: %t, was: %t", revision, drez.Data.(detectData).HasAppRevisionArg)
	}
	if drez.Data.(detectData).HasAppVersionArg != version {
		t.Errorf("Expected detected version arg: %t, was: %t", version, drez.Data.(detectData).HasAppVersionArg)
	}
}

func TestSplitBuildpackDetect(t *testing.T) {
	dr, err := testSBPDetect(t, "", nil)
	assertRejected(t, dr, err)

	dr, err = testSBPDetect(t, "ENV SOUS_RUN_IMAGE_SPEC=/sous-manifest.json", nil)
	assertAccepted(t, dr, err)
	assertArgs(t, dr, false, false)

	dr, err = testSBPDetect(t, `
	ENV SOUS_RUN_IMAGE_SPEC=/sous-manifest.json
	ARG APP_VERSION
	ARG APP_REVISION
	`, nil)
	assertAccepted(t, dr, err)
	assertArgs(t, dr, true, true)

	dr, err = testSBPDetect(t, "FROM docker.opentable.com/blub-builder:1.2.3", nil)
	assertRejected(t, dr, err)

	dr, err = testSBPDetect(t, "FROM docker.opentable.com/blub-builder:1.2.3",
		map[string]docker_registry.Metadata{".*blub-builder.*": {}})
	assertRejected(t, dr, err)

	dr, err = testSBPDetect(t, "FROM docker.opentable.com/blub-builder:1.2.3",
		map[string]docker_registry.Metadata{
			".*blub-builder.*": {
				Env: map[string]string{"SOUS_RUN_IMAGE_SPEC": "/sous-manifest.json"}},
		})
	assertAccepted(t, dr, err)
	assertArgs(t, dr, false, false)

	dr, err = testSBPDetect(t, `
	FROM docker.opentable.com/blub-builder:1.2.3
	ARG APP_VERSION
	ARG APP_REVISION
	`,
		map[string]docker_registry.Metadata{
			".*blub-builder.*": {
				Env: map[string]string{"SOUS_RUN_IMAGE_SPEC": "/sous-manifest.json"}},
		})
	assertAccepted(t, dr, err)
	assertArgs(t, dr, true, true)

	// n.b. Docker does not record ARG lines in containers, so there's no way for
	// the build container to expose APP_VERSION or APP_REVISION
	// Perhaps we should consider ENVs for those?
}

func TestRunnableBuildpackBuildTemplating(t *testing.T) {
	sb := &runnableBuilder{
		RunSpec: SplitImageRunSpec{
			Image: sbmImage{From: "scratch"},
			Files: []sbmInstall{
				{Source: sbmFile{Dir: "src"}, Destination: sbmFile{Dir: "dest"}},
			},
			Exec: []string{"cat", "/etc/shadow"},
		},
		splitBuilder: &splitBuilder{
			context: &sous.BuildContext{
				Source: sous.SourceContext{
					RemoteURL:  "github.com/example/project",
					OffsetDir:  "",
					NearestTag: sous.Tag{Name: "1.2.3"},
					Revision:   "cabba9edeadbeef",
				},
			},
			//VersionConfig:  "APP_VERSION=1.2.3",
			//RevisionConfig: "APP_REVISION=cabba9edeadbeef",
		},
	}
	buf := &bytes.Buffer{}

	err := sb.templateDockerfileBytes(buf)
	if err != nil {
		t.Error(err)
	}
	dockerfile := buf.String()
	hasString := func(needle string) {
		if strings.Index(dockerfile, needle) == -1 {
			t.Errorf("No %q in dockerfile.", needle)
		}
	}
	hasString("FROM scratch")
	hasString("ENV APP_VERSION=1.2.3 APP_REVISION=cabba9edeadbeef")
	hasString("COPY dest dest")
	hasString(`CMD ["cat","/etc/shadow"]`)
	//hasString("LABEL com.opentable.sous.build-image=") //once we push the build image...
}

func TestRunspecLoadLegacyManifest(t *testing.T) {

	mBuf := bytes.NewBufferString(`{
  "image": {
    "type": "Docker",
    "from": "scratch"
  },
  "files": [
    {
      "source": { "dir": "/built"},
      "dest":   { "dir": "/"}
    }
  ],
  "exec": ["/sous-demo"]
	}`)

	runspec := &MultiImageRunSpec{}
	dec := json.NewDecoder(mBuf)
	dec.Decode(runspec)

	if runspec.Image.From != "scratch" {
		t.Error("RunSpec didn't load Image.From")
	}

	if len(runspec.Files) != 1 {
		t.Fatal("No files loaded")
	}
	if runspec.Files[0].Source.Dir != "/built" {
		t.Error("RunSpec didn't load Files[0].Source")
	}
	if runspec.Files[0].Destination.Dir != "/" {
		t.Error("RunSpec didn't load Files[0].Destination")
	}

	if len(runspec.Validate()) > 0 {
		t.Error("Expected RunSpec to validate")
	}

	nrs := runspec.Normalized()
	if len(nrs.Images) != 1 {
		t.Error("Normalized runspec doesn't have 1 Images [sic]")
	}
}

func TestRunspecLoadMultiManifest(t *testing.T) {

	mBuf := bytes.NewBufferString(`{
		"images": [
			{
				"image": {
					"type": "Docker",
					"from": "scratch"
				},
				"files": [
					{
						"source": { "dir": "/built"},
						"dest":   { "dir": "/"}
					}
				],
				"exec": ["/sous-demo"]
		  },
			{
				"image": {
					"type": "Docker",
					"from": "scratch"
				},
				"files": [
					{
						"source": { "dir": "/built-extra"},
						"dest":   { "dir": "/"}
					}
				],
				"exec": ["/sous-scratch"]
		  }
    ]
	}`)

	runspec := &MultiImageRunSpec{}
	dec := json.NewDecoder(mBuf)
	err := dec.Decode(runspec)
	if err != nil {
		t.Fatal(err)
	}

	if len(runspec.Images) != 2 {
		t.Fatal("runspec doesn't have 2 Images [sic]")
	}

	if runspec.Images[0].Image.From != "scratch" {
		t.Error("RunSpec didn't load Image.From")
	}

	if len(runspec.Images[0].Files) != 1 {
		t.Fatal("No files loaded")
	}
	if runspec.Images[0].Files[0].Source.Dir != "/built" {
		t.Error("RunSpec didn't load Files[0].Source")
	}
	if runspec.Images[0].Files[0].Destination.Dir != "/" {
		t.Error("RunSpec didn't load Files[0].Destination")
	}

	if len(runspec.Validate()) > 0 {
		t.Error("Expected RunSpec to validate")
	}

	nrs := runspec.Normalized()
	if len(nrs.Images) != 2 {
		t.Error("Normalized runspec doesn't have 2 Images [sic]")
	}

	runspec.SplitImageRunSpec = &SplitImageRunSpec{
		Image: sbmImage{From: "scratch"},
	}

	if len(runspec.Validate()) == 0 {
		t.Error("Expected RunSpec not to validate with mixed legacy/new data")
	}

}
func TestRunSpecValidate(t *testing.T) {
	rs := &SplitImageRunSpec{}
	flaws := rs.Validate()
	if len(flaws) != 4 {
		t.Errorf("Expected %d flaws, got %d", 4, len(flaws))
	}
}
