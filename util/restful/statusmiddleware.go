package restful

import (
	"fmt"
	"io"
	"net/http"
	"runtime/debug"

	"github.com/opentable/sous/util/logging"
	"github.com/opentable/sous/util/logging/messages"
)

type (

	// A StatusMiddleware processes panics into 500s and other status codes.
	StatusMiddleware struct {
		gatelatch string
		logging.LogSink
	}
)

func (ph *StatusMiddleware) errorBody(status int, rq *http.Request, w io.Writer, data interface{}, err error, stack []byte) {
	if ph.gatelatch == "" {
		if data != nil {
			w.Write([]byte(fmt.Sprintf("%v\n", data)))
		}
		return
	}

	if header := rq.Header.Get("X-Gatelatch"); header != ph.gatelatch {
		if data != nil {
			w.Write([]byte(fmt.Sprintf("%s\n", data)))
		}
		messages.ReportLogFieldsMessage("Gatelatch header didn't match gatelatch env", logging.WarningLevel, ph.LogSink, ph.gatelatch, header)
		return
	}

	w.Write([]byte(fmt.Sprintf("Error status: %d\n", status)))
	w.Write([]byte(fmt.Sprintf("Data: %#v\n", data)))
	w.Write([]byte(fmt.Sprintf("Error: %+v\n", err)))

	if stack == nil {
		w.Write([]byte("Created stack: \n"))
		w.Write(debug.Stack())
	} else {
		w.Write([]byte("Passed (panic) stack: \n"))
		w.Write(stack)
	}
	return
}

// HandleResponse returns empty responses.
// It uses the LogSet provided by the graph.
func (ph *StatusMiddleware) HandleResponse(status int, r *http.Request, w http.ResponseWriter, data interface{}) {
	w.WriteHeader(status)
	ph.errorBody(status, r, w, data, nil, nil)
}

// HandlePanic returns a 500 and logs the error.
// It uses the LogSet provided by the graph.
func (ph *StatusMiddleware) HandlePanic(w http.ResponseWriter, r *http.Request, recovered interface{}) {
	w.WriteHeader(http.StatusInternalServerError)
	stack := debug.Stack()
	if ph.LogSink == nil {
		ph.LogSink = &fallbackLogger{}
	}
	messages.ReportLogFieldsMessage("Recovered, returned 500", logging.WarningLevel, ph.LogSink, recovered)
	ph.errorBody(http.StatusInternalServerError, r, w, "panicked - see logs", recovered.(error), stack)
}
