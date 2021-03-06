package messages

import (
	"fmt"
	"testing"

	"github.com/opentable/sous/util/logging"
	"github.com/stretchr/testify/assert"
)

func TestSomething(t *testing.T) {
	msg := buildLogFieldsMessage("sting", false, false, logging.InformationLevel)
	msg.EachField(logging.FieldReportFn(func(n logging.FieldName, v interface{}) {
		t.Log(n, v)
	}))
}

func TestReportLogFieldsMessage_Complete(t *testing.T) {
	logging.AssertReportFields(t,
		func(ls logging.LogSink) {
			cfg := logging.Config{}
			cfg.Kafka.Topic = "test-topic"
			cfg.Kafka.BrokerList = "broker1,broker2,broker3"

			ReportLogFieldsMessage("This is test message", logging.DebugLevel, ls, cfg)
		},
		logging.StandardVariableFields,
		map[string]interface{}{
			"@loglov3-otl":       logging.SousGenericV1,
			"severity":           logging.DebugLevel,
			"call-stack-message": "This is test message",
			"sous-fields":        "Basic,Kafka,Graphite,Config,Level,DisableConsole,ExtraConsole,Enabled,DefaultLevel,Topic,Brokers,BrokerList,Server",
			"sous-types":         "Config,string,bool",
			"json-value":         "{\"message\":{\"array\":[\"(logging.Config) {\\n Basic: (struct { Level string \\\"env:\\\\\\\"SOUS_LOGGING_LEVEL\\\\\\\"\\\"; DisableConsole bool; ExtraConsole bool \\\"env:\\\\\\\"SOUS_EXTRA_CONSOLE\\\\\\\"\\\" }) {\\n  Level: (string) \\\"\\\",\\n  DisableConsole: (bool) false,\\n  ExtraConsole: (bool) false\\n },\\n Kafka: (struct { Enabled bool; DefaultLevel string \\\"env:\\\\\\\"SOUS_KAFKA_LOG_LEVEL\\\\\\\"\\\"; Topic string \\\"env:\\\\\\\"SOUS_KAFKA_TOPIC\\\\\\\"\\\"; Brokers []string; BrokerList string \\\"env:\\\\\\\"SOUS_KAFKA_BROKERS\\\\\\\"\\\" }) {\\n  Enabled: (bool) false,\\n  DefaultLevel: (string) \\\"\\\",\\n  Topic: (string) (len=10) \\\"test-topic\\\",\\n  Brokers: ([]string) \\u003cnil\\u003e,\\n  BrokerList: (string) (len=23) \\\"broker1,broker2,broker3\\\"\\n },\\n Graphite: (struct { Enabled bool; Server string \\\"env:\\\\\\\"SOUS_GRAPHITE_SERVER\\\\\\\"\\\" }) {\\n  Enabled: (bool) false,\\n  Server: (string) \\\"\\\"\\n }\\n}\\n\"]}}",
			"sous-ids":           "",
			"sous-id-values":     "",
		})
}

func TestReportLogFieldsMessage_NoInterface(t *testing.T) {
	logging.AssertReportFields(t,
		func(ls logging.LogSink) {
			ReportLogFieldsMessage("This is test message no interface", logging.DebugLevel, ls)
		},
		logging.StandardVariableFields,
		map[string]interface{}{
			"@loglov3-otl":       logging.SousGenericV1,
			"severity":           logging.DebugLevel,
			"call-stack-message": "This is test message no interface",
			"sous-fields":        "",
			"sous-types":         "",
			"sous-ids":           "",
			"sous-id-values":     "",
		})
}

func TestReportLogFieldsMessage_String(t *testing.T) {
	logging.AssertReportFields(t,
		func(ls logging.LogSink) {
			ReportLogFieldsMessage("This is test message passing just a string", logging.DebugLevel, ls, "simple string")
		},
		logging.StandardVariableFields,
		map[string]interface{}{
			"@loglov3-otl":       logging.SousGenericV1,
			"severity":           logging.DebugLevel,
			"call-stack-message": "This is test message passing just a string",
			"sous-fields":        "",
			"sous-types":         "string",
			"json-value":         "{\"message\":{\"array\":[\"{\\\"string\\\":{\\\"string\\\":\\\"simple string\\\"}}\"]}}",
			"sous-ids":           "",
			"sous-id-values":     "",
		})
}

func TestReportLogFieldsMessage_StructAndString(t *testing.T) {
	logging.AssertReportFields(t,
		func(ls logging.LogSink) {
			cfg := logging.Config{}
			cfg.Kafka.Topic = "test-topic"
			cfg.Kafka.BrokerList = "broker1,broker2,broker3"

			ReportLogFieldsMessage("This is test message", logging.DebugLevel, ls, cfg, "simple string")
		},
		logging.StandardVariableFields,
		map[string]interface{}{
			"@loglov3-otl":       logging.SousGenericV1,
			"severity":           logging.DebugLevel,
			"call-stack-message": "This is test message",
			"sous-types":         "Config,string,bool",
			"json-value":         "{\"message\":{\"array\":[\"(logging.Config) {\\n Basic: (struct { Level string \\\"env:\\\\\\\"SOUS_LOGGING_LEVEL\\\\\\\"\\\"; DisableConsole bool; ExtraConsole bool \\\"env:\\\\\\\"SOUS_EXTRA_CONSOLE\\\\\\\"\\\" }) {\\n  Level: (string) \\\"\\\",\\n  DisableConsole: (bool) false,\\n  ExtraConsole: (bool) false\\n },\\n Kafka: (struct { Enabled bool; DefaultLevel string \\\"env:\\\\\\\"SOUS_KAFKA_LOG_LEVEL\\\\\\\"\\\"; Topic string \\\"env:\\\\\\\"SOUS_KAFKA_TOPIC\\\\\\\"\\\"; Brokers []string; BrokerList string \\\"env:\\\\\\\"SOUS_KAFKA_BROKERS\\\\\\\"\\\" }) {\\n  Enabled: (bool) false,\\n  DefaultLevel: (string) \\\"\\\",\\n  Topic: (string) (len=10) \\\"test-topic\\\",\\n  Brokers: ([]string) \\u003cnil\\u003e,\\n  BrokerList: (string) (len=23) \\\"broker1,broker2,broker3\\\"\\n },\\n Graphite: (struct { Enabled bool; Server string \\\"env:\\\\\\\"SOUS_GRAPHITE_SERVER\\\\\\\"\\\" }) {\\n  Enabled: (bool) false,\\n  Server: (string) \\\"\\\"\\n }\\n}\\n\",\"{\\\"string\\\":{\\\"string\\\":\\\"simple string\\\"}}\"]}}",
			"sous-fields":        "Basic,Kafka,Graphite,Config,Level,DisableConsole,ExtraConsole,Enabled,DefaultLevel,Topic,Brokers,BrokerList,Server",
			"sous-ids":           "",
			"sous-id-values":     "",
		})
}

//normally wouldn't use this logger with http response, but this was just done to test logging of a very complex structure
//and ensure it didn't fail ot going to put json-value as expected, since it contains pointers that can change on run
//execution
func TestReportLogFieldsMessage_TwoStructs(t *testing.T) {
	logging.AssertReportFields(t,
		func(ls logging.LogSink) {
			cfg := logging.Config{}
			cfg.Kafka.Topic = "test-topic"
			cfg.Kafka.BrokerList = "broker1,broker2,broker3"
			res := buildHTTPResponse(t, "GET", "http://example.com/api?a=a", 200, 0, 123)
			ReportLogFieldsMessage("This is test message", logging.DebugLevel, ls, cfg, res)
		},
		append(logging.StandardVariableFields, "json-value"),
		map[string]interface{}{
			"@loglov3-otl":       logging.SousGenericV1,
			"severity":           logging.DebugLevel,
			"call-stack-message": "This is test message",
			"sous-types":         "Config,string,bool,*Response,int,Header,int64,*Request,*ConnectionState",
			"sous-fields":        "Basic,Kafka,Graphite,Config,Level,DisableConsole,ExtraConsole,Enabled,DefaultLevel,Topic,Brokers,BrokerList,Server,Status,StatusCode,Proto,ProtoMajor,ProtoMinor,Header,Body,ContentLength,TransferEncoding,Close,Uncompressed,Trailer,Request,TLS,Response",
			"sous-ids":           "",
			"sous-id-values":     "",
		})
}

type testSubmessage struct {
	field int
}

func (sm testSubmessage) EachField(f logging.FieldReportFn) {
	f("test-field", sm.field)
}

func TestReportLogFieldsMessage_Submessage(t *testing.T) {
	logging.AssertReportFields(t,
		func(ls logging.LogSink) {
			ReportLogFieldsMessage("Only a test", logging.DebugLevel, ls, testSubmessage{field: 42})
		},
		logging.StandardVariableFields,
		map[string]interface{}{
			"@loglov3-otl":        logging.SousGenericV1,
			"severity":            logging.DebugLevel,
			"call-stack-message":  "Only a test",
			"call-stack-function": "github.com/opentable/sous/util/logging/messages.TestReportLogFieldsMessage_Submessage",
			"sous-fields":         "",
			"sous-id-values":      "",
			"sous-ids":            "",
			"sous-types":          "",
			"test-field":          42,
			//"json-value":      // because testSubmessage is an EachFielder, it doesn't dump into the json-value field
		},
	)
}

func TestReportLogFieldsMessage_CyclicalReference(t *testing.T) {
	logging.AssertReportFields(t,
		func(ls logging.LogSink) {
			type Parent struct {
				Child   *Parent
				LogData string
			}

			myVar := Parent{}
			myVar.LogData = "Hello"
			myVar.Child = &myVar
			ReportLogFieldsMessageToConsole("This is test message", logging.DebugLevel, ls, myVar)
		},
		append(logging.StandardVariableFields, "json-value"),
		map[string]interface{}{
			"@loglov3-otl":       logging.SousGenericV1,
			"severity":           logging.DebugLevel,
			"call-stack-message": "This is test message",
			"sous-fields":        "Child,LogData,Parent",
			"sous-types":         "Parent,*Parent,string",
		})
}

func TestReportLogFieldsMessage_error(t *testing.T) {
	logging.AssertReportFields(t,
		func(ls logging.LogSink) {

			err := fmt.Errorf("error msg")
			ReportLogFieldsMessage("This is test message", logging.DebugLevel, ls, err)
		},
		logging.StandardVariableFields,
		map[string]interface{}{
			"@loglov3-otl":       logging.SousGenericV1,
			"severity":           logging.DebugLevel,
			"call-stack-message": "This is test message",
			"sous-fields":        "",
			"sous-types":         "error",
			"json-value":         "{\"message\":{\"array\":[\"{\\\"error\\\":{\\\"error\\\":\\\"error msg\\\"}}\"]}}",
			"sous-ids":           "",
			"sous-id-values":     "",
		})
}

type Location struct {
	Repo string
	Dir  string
}

type TestInnerID struct {
	Source Location
}

type TestID struct {
	TestInnerID TestInnerID
	Cluster     string
}

func TestExtractIDs_TwoIds(t *testing.T) {
	d := &TestID{
		TestInnerID: TestInnerID{
			Source: Location{
				Repo: "fake.tld/org/" + "project",
				Dir:  "down/here",
			},
		},
		Cluster: "test-cluster",
	}

	sf := assembleStrayFields(true, d)

	assert.Len(t, sf.ids, 2)
	assert.Len(t, sf.values, 2)
}

func TestExtractIDs_NoIds(t *testing.T) {
	foo := "hello"

	sf := assembleStrayFields(true, foo)

	assert.Len(t, sf.ids, 0)
	assert.Len(t, sf.values, 0)
}

//TestReportLogFieldsMessageWithIDs_TwoIds making json-value and id-values variable because ptr to ID, addresses change
//betweeen runs
func TestReportLogFieldsMessageWithIDs_TwoIds(t *testing.T) {
	variableFields := []string{"json-value", "sous-id-values"}
	logging.AssertReportFields(t,
		func(ls logging.LogSink) {
			d := &TestID{
				TestInnerID: TestInnerID{
					Source: Location{
						Repo: "fake.tld/org/" + "project",
						Dir:  "down/here",
					},
				},
				Cluster: "test-cluster",
			}

			ReportLogFieldsMessageWithIDs("This is test message", logging.DebugLevel, ls, d)
		},
		append(logging.StandardVariableFields, variableFields...),
		map[string]interface{}{
			"@loglov3-otl":       logging.SousGenericV1,
			"severity":           logging.DebugLevel,
			"call-stack-message": "This is test message",
			"sous-fields":        "TestInnerID,Cluster,TestID,Source,Repo,Dir,Location",
			"sous-types":         "*TestID,TestInnerID,Location,string",
			"sous-ids":           "TestID,TestInnerID",
		})
}
