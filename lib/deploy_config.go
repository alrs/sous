package sous

import (
	"fmt"

	"github.com/davecgh/go-spew/spew"
	"github.com/pkg/errors"
)

type (
	// DeployConfig represents the configuration of a deployment's tasks,
	// in a specific cluster. i.e. their resources, environment, and the number
	// of instances.
	DeployConfig struct {
		// Resources represents the resources each instance of this software
		// will be given by the execution environment.
		Resources Resources `yaml:",omitempty" validate:"keys=nonempty,values=nonempty"`
		// Metadata stores values about deployments for outside applications to use
		Metadata Metadata `yaml:",omitempty" validate:"keys=nonempty,values=nonempty"`
		// Env is a list of environment variables to set for each instance of
		// of this deployment. It will be checked for conflict with the
		// definitions found in State.Defs.EnvVars, and if not in conflict
		// assumes the greatest priority.
		Env `yaml:",omitempty" validate:"keys=nonempty,values=nonempty"`

		// No manifest uses this, it doesn't get sent to Singularity. If we want it we should bring it back.
		//Args []string `yaml:",omitempty" validate:"values=nonempty"`
		// NumInstances is a guide to the number of instances that should be
		// deployed in this cluster, note that the actual number may differ due
		// to decisions made by Sous. If set to zero, Sous will decide how many
		// instances to launch.
		NumInstances int
		// Volumes lists the volume mappings for this deploy
		Volumes Volumes
		// Startup containts healthcheck options for this deploy.
		Startup Startup `yaml:",omitempty"`
		// Schedule is a cronjob-format schedule for jobs.
		Schedule string
	}

	// A DeployConfigs is a map from cluster name to DeployConfig
	DeployConfigs map[string]DeployConfig

	// Env is a mapping of environment variable name to value, used to provision
	// single instances of an application.
	Env map[string]string

	// Metadata represents an opaque map of metadata - Sous is agnostic about
	// its contents, except to validate it against the top level schema.
	Metadata map[string]string

	// NilVolumeFlaw is used when DeployConfig.Volumes contains a nil.
	NilVolumeFlaw struct {
		*DeployConfig
	}
)

// Validate returns a slice of Flaws.
func (dc *DeployConfig) Validate() []Flaw {
	var flaws []Flaw

	for _, v := range dc.Volumes {
		if v == nil {
			flaws = append(flaws, &NilVolumeFlaw{DeployConfig: dc})
			break
		}
	}

	rezs := dc.Resources
	if dc.Resources == nil {
		flaws = append(flaws, NewFlaw("No Resources set for DeployConfig",
			func() error { dc.Resources = make(Resources); return nil }))
		rezs = make(Resources)
	}

	flaws = append(flaws, rezs.Validate()...)

	flaws = append(flaws, dc.Startup.Validate()...)

	for _, f := range flaws {
		f.AddContext("deploy config", dc)
	}

	return flaws
}

// AddContext simply discards all context - NilVolumeFlaw doesn't need it.
func (nvf *NilVolumeFlaw) AddContext(string, interface{}) {
}

// Repair removes any nil entries in DeployConfig.Volumes.
func (nvf *NilVolumeFlaw) Repair() error {
	newVs := nvf.DeployConfig.Volumes[:0]
	for _, v := range nvf.DeployConfig.Volumes {
		if v != nil {
			newVs = append(newVs, v)
		}
	}
	nvf.DeployConfig.Volumes = newVs
	return nil
}

// Repair implements Flawed for State
func (dc *DeployConfig) Repair(fs []Flaw) error {
	return errors.Errorf("Can't do nuffin with flaws yet")
}

func (dc *DeployConfig) String() string {
	return fmt.Sprintf("#%d %s %+v : %+v %+v", dc.NumInstances, spew.Sprintf("%v", dc.Startup), dc.Resources, dc.Env, dc.Volumes)
}

// Equal is used to compare DeployConfigs
func (dc *DeployConfig) Equal(o DeployConfig) bool {
	diff, _ := dc.Diff(o)
	return !diff
}

// Diff returns a list of differences between this and the other DeployConfig.
func (dc *DeployConfig) Diff(o DeployConfig) (bool, []string) {
	var diffs []string
	if dc.NumInstances != o.NumInstances {
		diffs = append(diffs, fmt.Sprintf("number of instances; this: %d; other: %d", dc.NumInstances, o.NumInstances))
	}
	// Only compare contents if length of either > 0.
	// This makes nil equal to zero-length map.
	if len(dc.Env) != 0 || len(o.Env) != 0 {
		if !dc.Env.Equal(o.Env) {
			diffs = append(diffs, fmt.Sprintf("env; this: %v; other: %v", dc.Env, o.Env))
		}
	}
	// Only compare contents if length of either > 0.
	// This makes nil equal to zero-length map.
	if len(dc.Metadata) != 0 || len(o.Metadata) != 0 {
		if !dc.Metadata.Equal(o.Metadata) {
			diffs = append(diffs, fmt.Sprintf("metadata; this: %v; other: %v", dc.Metadata, o.Metadata))
		}
	}
	// Only compare contents if length of either > 0.
	if len(dc.Resources) != 0 || len(o.Resources) != 0 {
		if !dc.Resources.Equal(o.Resources) {
			diffs = append(diffs, fmt.Sprintf("resources; this: %v; other: %v", dc.Resources, o.Resources))
		}
	}
	// Only compare contents if length of either > 0.
	if len(dc.Volumes) != 0 || len(o.Volumes) != 0 {
		if !dc.Volumes.Equal(o.Volumes) {
			diffs = append(diffs, fmt.Sprintf("volumes; this: %v; other: %v", dc.Volumes, o.Volumes))
		}
	}
	diffs = append(diffs, dc.Startup.diff(o.Startup)...)
	// TODO: Compare Args
	return len(diffs) == 0, diffs
}

// Clone returns a deep copy of this DeployConfig.
func (dc DeployConfig) Clone() (c DeployConfig) {
	c.NumInstances = dc.NumInstances
	c.Env = make(Env)
	for k, v := range dc.Env {
		c.Env[k] = v
	}
	c.Resources = make(Resources)
	for k, v := range dc.Resources {
		c.Resources[k] = v
	}
	if dc.Metadata != nil {
		c.Metadata = make(Metadata)
		for k, v := range dc.Metadata {
			c.Metadata[k] = v
		}
	}
	c.Volumes = dc.Volumes.Clone()
	c.Startup = dc.Startup
	c.Schedule = dc.Schedule

	return
}

// Equal compares Envs
func (e Env) Equal(o Env) bool {
	if len(e) != len(o) {
		return false
	}

	for name, value := range e {
		if ov, ok := o[name]; !ok || ov != value {
			return false
		}
	}
	return true
}

// Equal compares Metadatas
func (e Metadata) Equal(o Metadata) bool {
	if len(e) != len(o) {
		return false
	}

	for name, value := range e {
		if ov, ok := o[name]; !ok || ov != value {
			return false
		}
	}
	return true
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}

func flattenDeployConfigs(dcs []DeployConfig) DeployConfig {
	dc := DeployConfig{
		Resources: make(Resources),
		Env:       make(Env),
		Metadata:  make(Metadata),
	}
	for _, c := range dcs {
		if c.NumInstances != 0 {
			dc.NumInstances = c.NumInstances
			break
		}
	}
	for _, c := range dcs {
		if len(c.Volumes) != 0 {
			dc.Volumes = c.Volumes
			break
		}
	}
	for _, c := range dcs {
		if c.Schedule != "" {
			dc.Schedule = c.Schedule
			break
		}
	}
	for _, c := range dcs {
		for n, v := range c.Resources {
			if _, set := dc.Resources[n]; !set {
				dc.Resources[n] = v
			}
		}
		for n, v := range c.Env {
			if _, set := dc.Env[n]; !set {
				dc.Env[n] = v
			}
		}
		for n, v := range c.Metadata {
			if _, set := dc.Metadata[n]; !set {
				dc.Metadata[n] = v
			}
		}

		dc.Startup = c.Startup
	}
	return dc
}
