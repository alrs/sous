package sous

import (
	"context"
	"fmt"

	"github.com/opentable/sous/util/logging"
	"github.com/opentable/sous/util/logging/messages"
	"github.com/pkg/errors"
)

type nameResolver struct {
	registry Registry
	log      logging.LogSink
}

// ResolveNames resolves diffs.
func (d *DeployableChans) ResolveNames(ctx context.Context, r Registry, ls logging.LogSink) *DeployableChans {
	names := &nameResolver{registry: r, log: ls}

	return d.Pipeline(ctx, names)
}

func HandlePairsByRegistry(r Registry, dp *DeployablePair, ls logging.LogSink) (*DeployablePair, *DiffResolution) {
	names := &nameResolver{registry: r, log: ls}
	return names.HandlePairs(dp)
}

func (names *nameResolver) HandlePairs(dp *DeployablePair) (*DeployablePair, *DiffResolution) {
	intended := dp.Post
	action := dp.Kind().ResolveVerb()

	newImageName := dp.Post

	switch dp.Kind() {
	default:
		panic(fmt.Errorf("Unknown kind %v", dp.Kind()))
	case SameKind, RemovedKind:
		// don't care about docker names
	case AddedKind, ModifiedKind:
		var newImageNameResolution *DiffResolution
		newImageName, newImageNameResolution = resolveName(names.registry, intended, names.log)
		messages.ReportLogFieldsMessage("Deployment processed, needs artifact", logging.ExtraDebug1Level, names.log, dp.Kind(), intended)
		if err := newImageNameResolution; err != nil {
			messages.ReportLogFieldsMessage("Unable to perform action", logging.InformationLevel, names.log, action, intended.ID(), err)
			return nil, err
		}
		if newImageName == nil {
			messages.ReportLogFieldsMessage("Unable to perform action no artifact for SourceID", logging.InformationLevel, names.log, action, intended.ID(), intended.SourceID)
			return nil, &DiffResolution{
				DeploymentID: dp.ID(),
				Desc:         "not created",
				Error:        WrapResolveError(errors.Errorf("Unable to create new deployment %q: no artifact for SourceID %q", intended.ID(), intended.SourceID)),
			}
		}
	}

	return &DeployablePair{ExecutorData: dp.ExecutorData, name: dp.name, Prior: dp.Prior, Post: newImageName}, nil
}

func resolveName(r Registry, d *Deployable, log logging.LogSink) (*Deployable, *DiffResolution) {
	if d == nil {
		return nil, &DiffResolution{
			Error: &ErrorWrapper{error: fmt.Errorf("nil deployable")},
		}
	}
	art, err := guardImage(r, d.Deployment, log)
	if err != nil {
		return d, &DiffResolution{
			DeploymentID: d.ID(),
			Error:        &ErrorWrapper{error: err},
		}
	}
	d.BuildArtifact = art
	return d, nil
}

func guardImage(r Registry, d *Deployment, log logging.LogSink) (*BuildArtifact, error) {
	if d.NumInstances == 0 {
		messages.ReportLogFieldsMessage("Deployment has 0 instances, skipping artifact check", logging.InformationLevel, log, d.ID())
		return nil, nil
	}
	art, err := r.GetArtifact(d.SourceID)
	if err != nil {
		return nil, &MissingImageNameError{err}
	}
	for _, q := range art.Qualities {
		if q.Kind != "advisory" || q.Name == "" {
			continue
		}
		advisoryIsValid := false
		var allowedAdvisories []string
		if d.Cluster == nil {
			return nil, fmt.Errorf("nil cluster on deployment %q", d)
		}
		allowedAdvisories = d.Cluster.AllowedAdvisories
		for _, aa := range allowedAdvisories {
			if aa == q.Name {
				advisoryIsValid = true
				break
			}
		}
		if !advisoryIsValid {
			return nil, &UnacceptableAdvisory{q, &d.SourceID}
		}
	}
	return art, err
}
