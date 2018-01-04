package storage

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	sous "github.com/opentable/sous/lib"
	"github.com/opentable/sous/util/logging"
	"github.com/stretchr/testify/suite"
	// it's a SQL db driver. This is how you do that.
	_ "github.com/lib/pq"
)

type PostgresStateManagerSuite struct {
	suite.Suite
	manager *PostgresStateManager
	db      *sql.DB
}

func (suite *PostgresStateManagerSuite) SetupTest() {
	var err error

	port := "6543"
	if np, set := os.LookupEnv("PGPORT"); set {
		port = np
	}
	connstr := fmt.Sprintf("dbname=sous_test_template host=localhost port=%s sslmode=disable", port)
	setupDB, err := sql.Open("postgres", connstr)
	if err != nil {
		suite.FailNow("Error setting up test database", "Error: %v. Did you already `make postgres-test-prepare`?", err)
	}
	// ignoring error because I think "no such DB is a failure"
	setupDB.Exec("drop database sous_test")
	if _, err := setupDB.Exec("create database sous_test template sous_test_template"); err != nil {
		suite.FailNow("Error creating test database", "connstr %q err %v", connstr, err)
	}
	if err := setupDB.Close(); err != nil {
		suite.FailNow("Error closing DB manipulation connection", "connstr %q err %v", connstr, err)
	}

	suite.manager, err = NewPostgresStateManager(PostgresConfig{
		DBName:   "sous_test",
		User:     "",
		Password: "",
		Host:     "localhost",
		Port:     port,
		SSL:      false,
	}, logging.SilentLogSet())
	//}, logging.Log)
	//logging.Log.BeChatty()

	if err != nil {
		suite.FailNow("Setting up", "error: %v", err)
	}

	connstr = fmt.Sprintf("dbname=sous_test host=localhost port=%s sslmode=disable", port)
	if suite.db, err = sql.Open("postgres", connstr); err != nil {
		suite.FailNow("Error establishing test-assertion DB connection.", "Error: %v", err)
	}
}

func (suite *PostgresStateManagerSuite) PluckSQL(sql string) interface{} {
	var v interface{}

	row := suite.db.QueryRow(sql)
	err := row.Scan(&v)
	suite.Require().NoError(err)

	return v
}

func (suite *PostgresStateManagerSuite) TestWriteState_success() {
	s := exampleState()

	suite.Require().NoError(suite.manager.WriteState(s, testUser))
	suite.Equal(int64(2), suite.PluckSQL("select count(*) from deployments"))
	suite.Require().NoError(suite.manager.WriteState(s, testUser))
	// Want to be sure that the deployments history doesn't vacuously grow.
	suite.Equal(int64(2), suite.PluckSQL("select count(*) from deployments"))

	ns, err := suite.manager.ReadState()
	suite.Require().NoError(err)

	oldD, err := s.Deployments()
	suite.Require().NoError(err)
	newD, err := ns.Deployments()
	suite.Require().NoError(err)

	for diff := range oldD.Diff(newD).Pairs {
		switch diff.Kind() {
		default:
			suite.Fail("Difference detected between written and read states", "They are: %s %+#v", diff.Kind(), diff)
		case sous.ModifiedKind:
			suite.Fail("Difference detected between written and read states", "%+#v %+#v", diff, diff.Diffs())

		case sous.SameKind:
		}
	}
}

func TestPostgresStateManager(t *testing.T) {
	suite.Run(t, new(PostgresStateManagerSuite))
}
