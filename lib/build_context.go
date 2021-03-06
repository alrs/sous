package sous

import (
	"os"
	"os/user"

	"github.com/opentable/sous/util/shell"
)

type (
	// BuildContext contains all the data required to perform a build.
	BuildContext struct {
		Sh         shell.Shell
		Source     SourceContext
		Scratch    ScratchContext
		Machine    Machine
		User       user.User
		Changes    Changes
		Advisories Advisories
	}

	// ScratchContext represents an isolated copy of a project's source code
	// somewhere on the host machine running Sous.
	ScratchContext struct {
		Sh                 *shell.Sh
		RootDir, OffsetDir string
	}

	// Machine represents a specific computer.
	Machine struct {
		Host, FullHost string
	}

	// Changes represents a set of changes that have happened since this project
	// was last built on the current machine by the current user.
	Changes struct {
		SousUpdated, NewCommit, NewTag, NewFiles, ChangedFiles []string
	}
)

// ShouldPullDuringBuild determines if Sous should pull updates to base images
// during a build.
func (bc *BuildContext) ShouldPullDuringBuild() bool {
	return !bc.Source.DevBuild && os.Getenv("SOUS_BUILD_NOPULL") != "YES"
}

// Version returns the SourceID for this build.
func (bc *BuildContext) Version() SourceID {
	return bc.Source.Version()
}

// RevID returns bc.Source.Revision.
func (bc *BuildContext) RevID() string {
	return bc.Source.Revision
}
