package cli

import (
	"os"

	"github.com/opentable/sous/config"
	"github.com/opentable/sous/graph"
	"github.com/opentable/sous/lib"
	"github.com/opentable/sous/util/cmdr"
)

// SousQueryGDM is the description of the `sous query gdm` command
type SousQueryGDM struct {
	StateManager *graph.ClientStateManager
	flags        struct {
		singularity string
		registry    string
	}
}

func init() { QuerySubcommands["gdm"] = &SousQueryGDM{} }

const sousQueryGDMHelp = `The intended state of deployment for every project and every cluster known to Sous.

The results of 'sous query gdm' and 'sous query ads' will not be identical if
a problem is preventing sous from modifying the current state of Singularity.
`

// Help prints the help
func (*SousQueryGDM) Help() string { return sousQueryGDMHelp }

// RegisterOn adds options set by flags to the injection graph.
func (*SousQueryGDM) RegisterOn(psy Addable) {
	psy.Add(graph.DryrunNeither)
	psy.Add(&config.DeployFilterFlags{})
}

// Execute defines the behavior of `sous query gdm`
func (sb *SousQueryGDM) Execute(args []string) cmdr.Result {

	state, err := sb.StateManager.ReadState()
	if err != nil {
		return EnsureErrorResult(err)
	}
	deployments, err := state.Deployments()
	if err != nil {
		return EnsureErrorResult(err)
	}

	sous.DumpDeployments(os.Stdout, deployments)
	return cmdr.Success()
}
