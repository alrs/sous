package graph

import (
	"sync"

	"github.com/opentable/sous/ext/docker"
	"github.com/pkg/errors"
)

func newLazyNameCache(cfg LocalSousConfig, mdb MaybeDatabase, ls LogSink, cl LocalDockerClient) lazyNameCache {
	return func() (*docker.NameCache, error) {
		theNameCacheOnce.Do(func() {
			theNameCache, theNameCacheErr = generateNameCache(cfg, mdb, ls, cl)
		})
		return theNameCache, theNameCacheErr
	}
}

func newNameCache(f lazyNameCache) (*docker.NameCache, error) {
	return f()
}

type lazyNameCache func() (*docker.NameCache, error)

var theNameCacheOnce sync.Once
var theNameCache *docker.NameCache
var theNameCacheErr error

// generateNameCache generates a brand new *docker.NameCache.
func generateNameCache(cfg LocalSousConfig, mdb MaybeDatabase, ls LogSink, cl LocalDockerClient) (*docker.NameCache, error) {
	if mdb.Err != nil {
		return nil, errors.Wrap(mdb.Err, "building name cache DB")
	}
	drh := cfg.Docker.RegistryHost
	return docker.NewNameCache(drh, cl.Client, ls.Child("docker-images"), mdb.Db)
}
