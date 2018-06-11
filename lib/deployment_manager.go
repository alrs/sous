package sous

import (
	"github.com/nyarly/spies"
	"github.com/opentable/sous/util/logging"
	"github.com/pkg/errors"
)

type (
	// A DeploymentManager allows the loading and storing of individual Deployments.
	DeploymentManager interface {
		ReadDeployment(did DeploymentID) (*Deployment, error)
		WriteDeployment(dep *Deployment, user User) error
	}

	deploymentManagerSpy struct {
		*spies.Spy
	}

	deploymentManagerDecorator struct {
		// anonymous so that the deploymentManagerDecorator can also be used as a StateManager
		StateManager
		log logging.LogSink
	}
)

// NewDeploymentManagerSpy returns a spy for testing purposes.
func NewDeploymentManagerSpy() (DeploymentManager, *spies.Spy) {
	spy := &spies.Spy{}

	return deploymentManagerSpy{spy}, spy
}

func (spy deploymentManagerSpy) ReadDeployment(did DeploymentID) (*Deployment, error) {
	res := spy.Called(did)
	if res.Get(0) == nil {
		return nil, res.Error(1)
	}

	return res.Get(0).(*Deployment), res.Error(1)
}

func (spy deploymentManagerSpy) WriteDeployment(dep *Deployment, user User) error {
	res := spy.Called(dep, user)
	return res.Error(0)
}

// MakeDeploymentManager wraps a StateManager such that it fulfills the DeploymentManager interface
func MakeDeploymentManager(sm StateManager, ls logging.LogSink) DeploymentManager {
	return &deploymentManagerDecorator{StateManager: sm, log: ls}
}

// ReadDeployment implements DeploymentManager on deploymentManagerDecorator
func (dm *deploymentManagerDecorator) ReadDeployment(did DeploymentID) (*Deployment, error) {
	state, err := dm.ReadState()
	if err != nil {
		return nil, err
	}

	deps, err := state.Deployments()
	if err != nil {
		return nil, err
	}

	dep, has := deps.Get(did)
	if !has {
		return nil, errors.Errorf("no deployment found for %s", did)
	}

	return dep, nil
}

func (dm *deploymentManagerDecorator) WriteDeployment(dep *Deployment, user User) error {
	state, err := dm.ReadState()
	if err != nil {
		return err
	}

	deps, err := state.Deployments()
	if err != nil {
		return err
	}
	deps.Set(dep.ID(), dep)
	return dm.WriteState(state, user)
}
