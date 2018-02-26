package sous

import "github.com/nyarly/spies"

type (
	// ClusterManager reads and writes deployments as scoped by cluster
	ClusterManager interface {
		ReadCluster(clusterName string) (Deployments, error)
		WriteCluster(clusterName string, deps Deployments, user User) error
	}

	clusterManagerSpy struct {
		*spies.Spy
	}

	clusterManagerDecorator struct {
		sm StateManager
	}
)

// NewClusterManagerSpy produces a spy/controller pair for ClusterManager
func NewClusterManagerSpy() (ClusterManager, *spies.Spy) {
	spy := &spies.Spy{}

	return clusterManagerSpy{spy}, spy
}

func (cm clusterManagerSpy) ReadCluster(clusterName string) (Deployments, error) {
	res := cm.Called(clusterName)
	return res.Get(0).(Deployments), res.Error(1)
}

func (cm clusterManagerSpy) WriteCluster(clusterName string, deps Deployments, user User) error {
	return cm.Called(clusterName, deps).Error(0)
}

// MakeClusterManager wraps a StateManager in a ClusterManager. This is the easy way to get a ClusterManager;
// It's assumed that more effecient ClusterManager implementations could be added to specific StateManagers.
func MakeClusterManager(sm StateManager) ClusterManager {
	return &clusterManagerDecorator{sm: sm}
}

// ReadCluster implements ClusterManager on the MakeClusterManager implementation
func (deco *clusterManagerDecorator) ReadCluster(clusterName string) (Deployments, error) {
	state, err := deco.sm.ReadState()
	if err != nil {
		return NewDeployments(), err
	}

	deps, err := state.Deployments()
	if err != nil {
		return NewDeployments(), err
	}
	return deps.Filter(func(d *Deployment) bool {
		return d.ClusterName == clusterName
	}), nil
}

// WriteCluster implements ClusterManager on the MakeClusterManager implementation
func (deco *clusterManagerDecorator) WriteCluster(clusterName string, wds Deployments, user User) error {
	state, err := deco.sm.ReadState()
	if err != nil {
		return err
	}

	deps, err := state.Deployments()
	if err != nil {
		return err
	}
	//cut out the deps we know about with the supplied name...
	deps = deps.Filter(func(d *Deployment) bool {
		return d.ClusterName != clusterName
	})
	ds := []*Deployment{}
	for _, d := range deps.Merge(wds).Snapshot() {
		ds = append(ds, d)
	}
	err = state.UpdateDeployments(ds...)
	if err != nil {
		return err
	}

	return deco.sm.WriteState(state, user)

}
