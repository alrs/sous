package sous

import (
	"fmt"
	"strings"
)

//go:generate ggen cmap.CMap(cmap.go) sous.DeployStates(deploystates.go) CMKey:DeployID Value:*DeployState
//go:generate stringer -type=DeployStatus

// A DeployState represents the state of a deployment in an external cluster.
// It wraps Deployment and adds Status.
type DeployState struct {
	Deployment
	Status          DeployStatus
	ExecutorMessage string
	ExecutorData    interface{}
	SchedulerURL    string
}

func (ds DeployState) String() string {
	return fmt.Sprintf("DEPLOYMENT:%s STATUS:%s EXECUTORDATA:%v", ds.Deployment.String(), ds.Status, ds.ExecutorData)
}

// TabbedDeployStatusHeaders returns the names of the fields for Tabbed, suitable
// for use with text/tabwriter.
func TabbedDeployStatusHeaders() string {
	return strings.Join([]string{TabbedDeploymentHeaders(), "Status"}, "\t")
}

// Tabbed returns the fields of a DeployState formatted in a tab delimited list.
func (ds *DeployState) Tabbed() string {
	return strings.Join([]string{ds.Deployment.Tabbed(), ds.Status.String()}, "\t")
}

// Clone returns an independent clone of this DeployState.
func (ds DeployState) Clone() *DeployState {
	ds.Deployment = *ds.Deployment.Clone()
	return &ds
}

// IgnoringStatus returns a Deployments containing all the nested deployments
// in this DeployStates.
func (ds DeployStates) IgnoringStatus() Deployments {
	deployments := NewDeployments()
	for key, value := range ds.Snapshot() {
		deployments.Set(key, &value.Deployment)
	}
	return deployments
}

// Final reports whether we should expect this DeployState to be finished -
// in other words, DeployState.Final() -> false implies that a subsequent
// DeployState will have a different status; polling components will want to poll again.
func (ds DeployState) Final() bool {
	switch ds.Status {
	default:
		return false
	case DeployStatusActive, DeployStatusFailed:
		return true
	}
}

// Diff computes the list of differences between two DeployStates and returns
// "true" if they're different, along with a list of differences
func (ds *DeployState) Diff(o *DeployState) (bool, []string) {
	// XXX uses deployment.Diff
	_, depS := ds.Deployment.Diff(&o.Deployment)

	if ds.Status != o.Status {
		depS = append(depS, fmt.Sprintf("status: this: %s other: %s", ds.Status, o.Status))
	}
	return len(depS) > 0, depS
}
