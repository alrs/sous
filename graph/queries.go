package graph

import (
	"github.com/opentable/sous/cli/queries"
	sous "github.com/opentable/sous/lib"
)

func newArtifactQuery(c HTTPClient, u sous.User) queries.ArtifactQuery {
	return queries.ArtifactQuery{
		Client: c.HTTPClient,
		User:   u,
	}
}

func newDeploymentQuery(sm *ClientStateManager, aq queries.ArtifactQuery) queries.Deployment {
	return queries.Deployment{
		StateManager:  sm,
		ArtifactQuery: aq,
	}
}
