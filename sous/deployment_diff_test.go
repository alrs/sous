package sous

import (
	"log"
	"testing"

	"github.com/samsalisbury/semv"
	"github.com/stretchr/testify/assert"
)

func TestEmptyDiff(t *testing.T) {
	assert := assert.New(t)

	intended := make(Deployments, 0)
	existing := make(Deployments, 0)

	dc := intended.Diff(existing)
	ds := dc.Collect()

	assert.Len(ds.New, 0)
	assert.Len(ds.Gone, 0)
	assert.Len(ds.Same, 0)
	assert.Len(ds.Changed, 0)
}

func makeDepl(repo string, num int) Deployment {
	version, _ := semv.Parse("1.1.1-latest")
	return Deployment{
		SourceVersion: SourceVersion{
			RepoURL:    RepoURL(repo),
			Version:    version,
			RepoOffset: "",
		},
		DeployConfig: DeployConfig{
			NumInstances: num,
			Env:          map[string]string{},
			Resources: map[string]string{
				"cpu":    ".1",
				"memory": "100",
				"ports":  "1",
			},
		},
		Owners: map[string]bool{"judson": true, "sam": true},
	}
}

func TestRealDiff(t *testing.T) {
	log.SetFlags(log.Flags() | log.Lshortfile)
	assert := assert.New(t)

	intended := make(Deployments, 0)
	existing := make(Deployments, 0)

	repoOne := "https://github.com/opentable/one"
	repoTwo := "https://github.com/opentable/two"
	repoThree := "https://github.com/opentable/three"
	repoFour := "https://github.com/opentable/four"

	intended = append(intended, makeDepl(repoOne, 1)) //remove

	existing = append(existing, makeDepl(repoTwo, 1)) //same
	intended = append(intended, makeDepl(repoTwo, 1)) //same

	existing = append(existing, makeDepl(repoThree, 1)) //changed
	intended = append(intended, makeDepl(repoThree, 2)) //changed

	existing = append(existing, makeDepl(repoFour, 1)) //create

	dc := intended.Diff(existing)
	ds := dc.Collect()

	if assert.Len(ds.Gone, 1) {
		assert.Equal(string(ds.Gone[0].RepoURL), repoOne)
	}

	if assert.Len(ds.Same, 1) {
		assert.Equal(string(ds.Same[0].RepoURL), repoTwo)
	}

	if assert.Len(ds.Changed, 1) {
		assert.Equal(repoThree, string(ds.Changed[0].name.source.RepoURL))
		assert.Equal(repoThree, string(ds.Changed[0].prior.RepoURL))
		assert.Equal(repoThree, string(ds.Changed[0].post.RepoURL))
		assert.Equal(ds.Changed[0].prior.NumInstances, 1)
		assert.Equal(ds.Changed[0].post.NumInstances, 2)
	}

	if assert.Len(ds.New, 1) {
		assert.Equal(string(ds.New[0].RepoURL), repoFour)
	}

}
