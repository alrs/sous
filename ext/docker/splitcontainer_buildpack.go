package docker

import (
	"io"
	"os"
	"path/filepath"

	"github.com/docker/docker/builder/dockerfile/parser"
	sous "github.com/opentable/sous/lib"
	"github.com/opentable/sous/util/docker_registry"
	"github.com/opentable/sous/util/firsterr"
	"github.com/opentable/sous/util/logging"
	"github.com/opentable/sous/util/logging/messages"
	"github.com/pkg/errors"
)

type (
	// A SplitBuildpack implements the pattern of using a build container and producing a separate deploy container
	SplitBuildpack struct {
		registry docker_registry.Client
		detected *sous.DetectResult
		log      logging.LogSink
	}
)

// SOUS_RUN_IMAGE_SPEC is the env name that build containers must declare with the path to their runspec file.
const SOUS_RUN_IMAGE_SPEC = "SOUS_RUN_IMAGE_SPEC"

// NewSplitBuildpack returns a new SplitBuildpack
func NewSplitBuildpack(r docker_registry.Client, ls logging.LogSink) *SplitBuildpack {
	return &SplitBuildpack{
		registry: r,
		log:      ls,
	}
}

func parseDocker(f io.Reader) (*parser.Node, error) {
	d := parser.Directive{LookingForDirectives: true}
	parser.SetEscapeToken(parser.DefaultEscapeToken, &d)

	return parser.Parse(f, &d)
}

func parseDockerfile(path string) (*parser.Node, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return parseDocker(f)
}

// Detect implements Buildpack on SplitBuildpack
func (sbp *SplitBuildpack) Detect(ctx *sous.BuildContext) (*sous.DetectResult, error) {
	dfPath := filepath.Join(ctx.Source.OffsetDir, "Dockerfile")
	if !ctx.Sh.Exists(dfPath) {
		return nil, errors.Errorf("%s does not exist", dfPath)
	}

	messages.ReportLogFieldsMessage("Inspecting Dockerfile", logging.DebugLevel, sbp.log, dfPath)

	ast, err := parseDockerfile(ctx.Sh.Abs(dfPath))
	if err != nil {
		return nil, err
	}

	detector := &splitDetector{
		rootAst:  ast,
		registry: sbp.registry,
		froms:    []*parser.Node{},
		envs:     []*parser.Node{},
		ls:       sbp.log,
	}

	// check the local image for dev build
	if ctx.Source.DevBuild {
		err = firsterr.Returned(
			detector.absorbDockerfile,
			detector.fetchFromRunSpec,
			func() error { return detector.checkLocalImage(ctx) },
			detector.processEnv,
		)
	} else {
		err = firsterr.Returned(
			detector.absorbDockerfile,
			detector.fetchFromRunSpec,
			detector.processEnv,
		)
	}

	sbp.detected = detector.result()

	return sbp.detected, err
}

// Build implements Buildpack on SplitBuildpack
func (sbp *SplitBuildpack) Build(ctx *sous.BuildContext) (*sous.BuildResult, error) {
	drez := sbp.detected
	script := splitBuilder{context: ctx, detected: drez, subBuilders: []*runnableBuilder{}}

	/*
			docker build <args> <offset> #-> Successfully build (image id)
			docker create <image id> #-> container id
			docker cp <container id>:<SOUS_RUN_IMAGE_SPEC> $TMPDIR/runspec.json
			[parse runspec]
			runspec file <- files @
			  docker cp <container id>:<file.sourcedir> $TMPDIR/<file.destdir>
		  in $TMPDIR docker build - < {templated Dockerfile} #-> Successfully built (image id)
	*/
	err := firsterr.Returned(
		script.begin,
		script.buildBuild,
		script.setupTempdir,
		script.createBuildContainer,
		script.extractRunSpec,
		script.validateRunSpec,
		script.constructImageBuilders,
		script.extractFiles,
		script.teardownBuildContainer,

		script.templateDockerfiles,
		script.buildRunnables,
	)

	return script.result(), err
}
