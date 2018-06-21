package singularity

import (
	"crypto/md5"
	"errors"
	"fmt"
	"testing"

	"github.com/opentable/sous/lib"
	"github.com/opentable/sous/util/logging"
	"github.com/stretchr/testify/require"
)

var requestID = "12345"

func TestDeployerMessage(t *testing.T) {
	logger, control := logging.NewLogSinkSpy()
	pair := baseDeployablePair()

	taskData := &singularityTaskData{
		requestID: requestID,
	}

	reportDeployerMessage("test", pair, nil, taskData, nil, logging.InformationLevel, logger)

	logCalls := control.CallsTo("Fields")
	require.Len(t, logCalls, 1)
	fields := logCalls[0].PassedArgs().Get(0).([]logging.EachFielder)

	logging.AssertMessageFieldlist(t, fields, logging.StandardVariableFields, defaultExpectedFields())

	//weak check on WriteToConsole
	// these messages don't mean anything to most operators.
	//   the ones who care can run with -d -v and get the raw logs.
	consoleCalls := control.CallsTo("Console")
	require.Len(t, consoleCalls, 0)
}

func TestDeployerMessageNilCheck(t *testing.T) {
	logger, control := logging.NewLogSinkSpy()

	reportDeployerMessage("test", nil, nil, nil, nil, logging.InformationLevel, logger)

	logCalls := control.CallsTo("Fields")
	logMessages := logCalls[0].PassedArgs().Get(0).([]logging.EachFielder)

	expectedFields := map[string]interface{}{
		"@loglov3-otl":       logging.SousRectifierSingularityV1,
		"severity":           logging.InformationLevel,
		"call-stack-message": "test",
		"sous-diffs":         "",
	}

	logging.AssertMessageFieldlist(t, logMessages, logging.StandardVariableFields, expectedFields)
}

func TestDeployerMessageError(t *testing.T) {
	logger, control := logging.NewLogSinkSpy()
	pair := baseDeployablePair()
	taskData := &singularityTaskData{
		requestID: requestID,
	}
	err := errors.New("Test error")
	expectedFields := defaultExpectedFields()
	expectedFields["error"] = "Test error"

	reportDeployerMessage("test", pair, nil, taskData, err, logging.InformationLevel, logger)

	logCalls := control.CallsTo("Fields")
	logMessages := logCalls[0].PassedArgs().Get(0).([]logging.EachFielder)

	logging.AssertMessageFieldlist(t, logMessages, logging.StandardVariableFields, expectedFields)
}

func TestDeployerMessageDiffs(t *testing.T) {
	logger, control := logging.NewLogSinkSpy()
	pair := baseDeployablePair()
	requestID := "12345"
	taskData := &singularityTaskData{
		requestID: requestID,
	}
	diffs := []string{"test", "test1", "test2"}
	expectedFields := defaultExpectedFields()
	expectedFields["sous-diffs"] = "test\ntest1\ntest2"

	reportDeployerMessage("test", pair, diffs, taskData, nil, logging.InformationLevel, logger)

	logCalls := control.CallsTo("Fields")
	fields := logCalls[0].PassedArgs().Get(0).([]logging.EachFielder)

	logging.AssertMessageFieldlist(t, fields, logging.StandardVariableFields, expectedFields)
}

func TestDiffResolutionMessage(t *testing.T) {
	logger, control := logging.NewLogSinkSpy()

	diffRes := sous.DiffResolution{
		DeploymentID: sous.DeploymentID{
			ManifestID: sous.ManifestID{
				Source: sous.SourceLocation{
					Repo: "repo/marker",
					Dir:  "dir/marker",
				},
				Flavor: "thai",
			},
			Cluster: "pp-sf",
		},
		Desc:  "description goes here",
		Error: sous.WrapResolveError(fmt.Errorf("bad")),
	}

	reportDiffResolutionMessage("test", diffRes, logging.InformationLevel, logger)

	logCalls := control.CallsTo("Fields")
	require.Len(t, logCalls, 1)
	fields := logCalls[0].PassedArgs().Get(0).([]logging.EachFielder)

	expectedFields := map[string]interface{}{
		"@loglov3-otl":                 logging.SousDiffResolution,
		"severity":                     logging.InformationLevel,
		"call-stack-message":           "test",
		"sous-deployment-id":           diffRes.DeploymentID.String(),
		"sous-manifest-id":             diffRes.DeploymentID.ManifestID.String(),
		"sous-resolution-description":  string(diffRes.Desc),
		"sous-resolution-errormessage": "bad",
		"sous-resolution-errortype":    "*errors.errorString",
	}

	logging.AssertMessageFieldlist(t, fields, logging.StandardVariableFields, expectedFields)

	//weak check on WriteToConsole
	// these messages don't mean anything to most operators.
	//   the ones who care can run with -d -v and get the raw logs.
	consoleCalls := control.CallsTo("Console")
	require.Len(t, consoleCalls, 0)
}

func defaultExpectedFields() map[string]interface{} {
	return map[string]interface{}{
		"@loglov3-otl":                          logging.SousRectifierSingularityV1,
		"severity":                              logging.InformationLevel,
		"call-stack-message":                    "test",
		"sous-request-id":                       requestID,
		"sous-diffs":                            "",
		"sous-deployment-id":                    ":",
		"sous-deployment-diffs":                 "No detailed diff because pairwise diff kind is \"same\"",
		"sous-diff-disposition":                 "same",
		"sous-manifest-id":                      "",
		"sous-post-artifact-name":               "the-post-image",
		"sous-post-artifact-qualities":          "",
		"sous-post-artifact-type":               "docker",
		"sous-post-checkready-failurestatuses":  "",
		"sous-post-checkready-interval":         0,
		"sous-post-checkready-portindex":        0,
		"sous-post-checkready-protocol":         "",
		"sous-post-checkready-retries":          0,
		"sous-post-checkready-uripath":          "",
		"sous-post-checkready-uritimeout":       0,
		"sous-post-clustername":                 "cluster",
		"sous-post-env":                         fmt.Sprintf("%s", md5.Sum([]byte("null"))),
		"sous-post-flavor":                      "",
		"sous-post-kind":                        "",
		"sous-post-metadata":                    "null",
		"sous-post-numinstances":                1,
		"sous-post-offset":                      "",
		"sous-post-owners":                      "",
		"sous-post-repo":                        "fake.tld/org/project",
		"sous-post-resources":                   "{}",
		"sous-post-startup-connectdelay":        0,
		"sous-post-startup-connectinterval":     0,
		"sous-post-startup-skipcheck":           false,
		"sous-post-startup-timeout":             0,
		"sous-post-status":                      "DeployStatusAny",
		"sous-post-tag":                         "0.0.0",
		"sous-post-volumes":                     "null",
		"sous-prior-artifact-name":              "the-prior-image",
		"sous-prior-artifact-qualities":         "",
		"sous-prior-artifact-type":              "docker",
		"sous-prior-checkready-failurestatuses": "",
		"sous-prior-checkready-interval":        0,
		"sous-prior-checkready-portindex":       0,
		"sous-prior-checkready-protocol":        "",
		"sous-prior-checkready-retries":         0,
		"sous-prior-checkready-uripath":         "",
		"sous-prior-checkready-uritimeout":      0,
		"sous-prior-clustername":                "cluster",
		"sous-prior-env":                        fmt.Sprintf("%s", md5.Sum([]byte("null"))),
		"sous-prior-flavor":                     "",
		"sous-prior-kind":                       "",
		"sous-prior-metadata":                   "null",
		"sous-prior-numinstances":               1,
		"sous-prior-offset":                     "",
		"sous-prior-owners":                     "",
		"sous-prior-repo":                       "fake.tld/org/project",
		"sous-prior-resources":                  "{}",
		"sous-prior-startup-connectdelay":       0,
		"sous-prior-startup-connectinterval":    0,
		"sous-prior-startup-skipcheck":          false,
		"sous-prior-startup-timeout":            0,
		"sous-prior-status":                     "DeployStatusAny",
		"sous-prior-tag":                        "0.0.0",
		"sous-prior-volumes":                    "null",
	}
}
