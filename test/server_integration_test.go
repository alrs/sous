package test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/opentable/sous/config"
	"github.com/opentable/sous/ext/storage"
	"github.com/opentable/sous/graph"
	sous "github.com/opentable/sous/lib"
	"github.com/opentable/sous/server"
	"github.com/opentable/sous/util/logging"
	"github.com/opentable/sous/util/restful"
	"github.com/pborman/uuid"
	"github.com/samsalisbury/psyringe"
	"github.com/samsalisbury/semv"
	"github.com/stretchr/testify/suite"
)

type (
	integrationServerTests struct {
		suite.Suite
		client restful.HTTPClient
		user   sous.User
		log    logging.LogSinkController
	}

	liveServerSuite struct {
		integrationServerTests
		server  *httptest.Server
		cleanup func()
	}

	inmemServerSuite struct {
		integrationServerTests
		cleanup func()
	}
)

// prepare returns a logging.LogSink and http.Handler for use in tests.
// It also returns a cleanup function which should be called to remove
// temp files created after each test run.
func (suite *integrationServerTests) prepare() (logging.LogSink, http.Handler, func()) {
	td, err := filepath.Abs("../ext/storage/testdata")
	if err != nil {
		suite.FailNow("setup failed: %s", err)
	}
	temp := filepath.Join(os.TempDir(), "soustests", uuid.New())
	sourcepath, remotepath, outpath :=
		filepath.Join(td, "in"),
		filepath.Join(temp, "remote"),
		filepath.Join(temp, "out")

	dsm := storage.NewDiskStateManager(sourcepath)
	s, err := dsm.ReadState()
	suite.Require().NoError(err)

	storage.PrepareTestGitRepo(suite.T(), s, remotepath, outpath)

	log, ctrl := logging.NewLogSinkSpy()
	suite.log = ctrl

	g := graph.TestGraphWithConfig(semv.Version{}, &bytes.Buffer{}, os.Stdout, os.Stdout, "StateLocation: '"+outpath+"'\n")
	g.Add(&config.Verbosity{})
	g.Add(&config.DeployFilterFlags{Cluster: "cluster-1"})
	g.Add(graph.DryrunBoth)

	testGraph := psyringe.TestPsyringe{Psyringe: g.Psyringe}
	testGraph.Replace(graph.LogSink{LogSink: log})
	/*
		state := &sous.State{}
		state.SetEtag("qwertybeatsdvorak")
		sm := sous.DummyStateManager{State: state}

		g.Add(
			func() graph.StateReader { return graph.StateReader{StateReader: &sm} },
			func() graph.StateWriter { return graph.StateWriter{StateWriter: &sm} },
			func() *graph.StateManager { return &graph.StateManager{StateManager: &sm} },
		)
	*/

	serverScoop := struct {
		Handler graph.ServerHandler
	}{}

	g.MustInject(&serverScoop)
	if serverScoop.Handler.Handler == nil {
		suite.FailNow("Didn't inject http.Handler!")
	}
	return log, serverScoop.Handler.Handler, func() {
		if err := os.RemoveAll(outpath); err != nil {
			suite.T().Errorf("cleanup failed: %s", err)
		}
		if err := os.RemoveAll(remotepath); err != nil {
			suite.T().Errorf("cleanup failed: %s", err)
		}
	}

}

func (suite *liveServerSuite) SetupTest() {
	lt, h, cleanup := suite.prepare()
	suite.cleanup = cleanup

	suite.server = httptest.NewServer(h)
	suite.user = sous.User{}

	var err error
	suite.integrationServerTests.client, err = restful.NewClient(suite.server.URL, lt)
	if err != nil {
		suite.FailNow("Error constructing client: %v", err)
	}
}

func (suite *inmemServerSuite) SetupTest() {
	lt, h, cleanup := suite.prepare()
	suite.cleanup = cleanup

	suite.user = sous.User{}
	var err error
	suite.integrationServerTests.client, err = restful.NewInMemoryClient(h, lt)
	if err != nil {
		suite.FailNow("Error constructing client: %v", err)
	}
}

func (suite liveServerSuite) TearDownTest() {
	suite.server.Close()
	if suite.cleanup != nil {
		suite.cleanup()
	}
}

func (suite inmemServerSuite) TearDownTest() {
	if suite.cleanup != nil {
		suite.cleanup()
	}
}

func (suite integrationServerTests) TestOverallRouter() {

	gdm := server.GDMWrapper{}
	updater, err := suite.client.Retrieve("./gdm", nil, &gdm, suite.user.HTTPHeaders())
	suite.NoError(err)

	suite.Len(gdm.Deployments, 2)
	suite.NotZero(updater)
}

func (suite integrationServerTests) TestUpdateServers() {
	data := server.ServerListData{}
	updater, err := suite.client.Retrieve("./servers", nil, &data, nil)

	suite.NoError(err)
	suite.Len(data.Servers, 0)

	newServers := server.ServerListData{
		Servers: []server.NameData{{ClusterName: "name", URL: "http://url"}},
	}

	err = updater.Update(&newServers, nil)
	suite.NoError(err)

	data = server.ServerListData{}
	_, err = suite.client.Retrieve("./servers", nil, &data, nil)
	suite.NoError(err)
	suite.Len(data.Servers, 1)
}

func (suite integrationServerTests) TestUpdateStateDeployments_Precondition() {
	data := server.GDMWrapper{Deployments: []*sous.Deployment{}}
	err := suite.client.Create("./state/deployments", nil, &data, nil)
	suite.Error(err, `412 Precondition Failed: "resource present for If-None-Match=*!\n"`)
}

func (suite integrationServerTests) TestUpdateStateDeployments_Update() {
	data := server.GDMWrapper{}

	updater, err := suite.client.Retrieve("./state/deployments", nil, &data, nil)
	suite.NoError(err)
	suite.Len(data.Deployments, 1)
	suite.NotNil(updater)

	data.Deployments = append(data.Deployments, sous.DeploymentFixture("sequenced-repo"))
	err = updater.Update(&data, nil)
	suite.NoError(err)

	_, err = suite.client.Retrieve("./state/deployments", nil, &data, nil)
	suite.NoError(err)
	suite.Len(data.Deployments, 2)
}

func TestLiveServerSuite(t *testing.T) {
	suite.Run(t, new(liveServerSuite))
}

func TestInMemoryServerSuite(t *testing.T) {
	suite.Run(t, new(inmemServerSuite))
}
