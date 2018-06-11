package graph

import (
	"fmt"

	sous "github.com/opentable/sous/lib"
	"github.com/pkg/errors"
)

// RefinedResolveFilter is a sous.ResolveFilter refined by user-requested flags.
type RefinedResolveFilter sous.ResolveFilter

// OT_ENV_FLAVOR is the environment name to use for flavors.
const OT_ENV_FLAVOR = "OT_ENV_FLAVOR"

func newRefinedResolveFilter(f *sous.ResolveFilter, discovered *SourceContextDiscovery) (*RefinedResolveFilter, error) {
	c := discovered.GetContext()
	if f == nil { // XXX I think this needs to be supplied anyway by consumers..
		f = &sous.ResolveFilter{}
	}

	rrf := &(*f)

	if rrf.Repo.All() {
		if c.PrimaryRemoteURL != "" {
			rrf.Repo = sous.NewResolveFieldMatcher(c.PrimaryRemoteURL)
		}
		if rrf.Offset.All() || *rrf.Offset.Match == "" {
			rrf.Offset = sous.NewResolveFieldMatcher(c.OffsetDir)
		}
		if f.Tag.All() && discovered.SourceContext != nil && discovered.TagVersion() != "" {
			rrf.SetTag(discovered.TagVersion())
		}
	} else {
		var offset string
		if rrf.Offset.Match != nil {
			offset = *rrf.Offset.Match
		}
		rrf.Offset = sous.NewResolveFieldMatcher(offset)
	}

	if rrf.Repo.All() {
		return nil, errors.Errorf("no repo specified, please use -repo or run sous inside a git repo with a configured remote")
	}

	return (*RefinedResolveFilter)(rrf), nil
}

func newTargetDeploymentID(rrf *RefinedResolveFilter) (TargetDeploymentID, error) {
	if rrf == nil {
		return TargetDeploymentID{}, fmt.Errorf("nil ResolveFilter")
	}
	repo, err := rrf.Repo.Value()
	if err != nil {
		return TargetDeploymentID{}, fmt.Errorf("repo: %s", err)
	}
	cluster, err := rrf.Cluster.Value()
	if err != nil {
		return TargetDeploymentID{}, fmt.Errorf("Setting up target deployment: cluster: %s", err)
	}

	return TargetDeploymentID{
		ManifestID: sous.ManifestID{
			Source: sous.SourceLocation{
				Repo: repo,
				Dir:  rrf.Offset.ValueOr(""),
			},
			Flavor: rrf.Flavor.ValueOr(""),
		},
		Cluster: cluster,
	}, nil
}

func newTargetManifestID(rrf *RefinedResolveFilter) (TargetManifestID, error) {
	if rrf == nil {
		return TargetManifestID{}, fmt.Errorf("nil ResolveFilter")
	}
	repo, err := rrf.Repo.Value()
	if err != nil {
		return TargetManifestID{}, fmt.Errorf("repo: %s", err)
	}

	return TargetManifestID{
		Source: sous.SourceLocation{
			Repo: repo,
			Dir:  rrf.Offset.ValueOr(""),
		},
		Flavor: rrf.Flavor.ValueOr(""),
	}, nil
}

func newTargetManifest(auto userSelectedOTPLDeployManifest, tmid TargetManifestID, sm *ClientStateManager) (TargetManifest, error) {
	s, err := sm.ReadState()
	if err != nil {
		return TargetManifest{}, err
	}
	mid := sous.ManifestID(tmid)
	m, ok := s.Manifests.Get(mid)

	if ok {
		return TargetManifest{m}, nil
	}

	var deploySpecs sous.DeploySpecs
	if auto.Manifest != nil {
		deploySpecs = auto.Manifest.Deployments
		m = auto.Clone()
	}
	if m == nil {
		m = &sous.Manifest{}
	}
	if len(deploySpecs) == 0 {
		deploySpecs = defaultDeploySpecs(s.Defs.Clusters)
	}

	if tmid.Flavor != "" {
		for _, d := range deploySpecs {
			d.Env[OT_ENV_FLAVOR] = tmid.Flavor
		}
	}

	m.Deployments = deploySpecs
	m.SetID(mid)

	fls := m.Validate()
	sous.RepairAll(fls)
	return TargetManifest{m}, nil
}

// QueryMap returns a map suitable for use with the HTTP API.
// xxx This needed to be defined on both TargetManifestID and sous.ManifestID - I don't understand why.
func (mid TargetManifestID) QueryMap() map[string]string {
	manifestQuery := map[string]string{}
	manifestQuery["repo"] = mid.Source.Repo
	manifestQuery["offset"] = mid.Source.Dir
	manifestQuery["flavor"] = mid.Flavor
	return manifestQuery
}

func defaultDeploySpecs(clusters sous.Clusters) sous.DeploySpecs {
	defaults := sous.DeploySpecs{}
	for name := range clusters {
		spec := sous.DeploySpec{
			DeployConfig: sous.DeployConfig{
				Resources: sous.Resources{},
				Env:       map[string]string{},
				Startup: sous.Startup{
					// should be defaults in the clusters, but we want to make these clear and explicit
					CheckReadyProtocol: "HTTP",
					CheckReadyURIPath:  "/health",
				},
				// XXX Should be 0 - used when no config has been specified
				NumInstances: 1,
			},
		}

		// repairing the validation flaws on a fresh DeploySpec sets defaults.
		// more importantly, this is a single consistent way to set those defaults.
		sous.RepairAll(spec.Validate())
		defaults[name] = spec
	}
	return defaults
}
