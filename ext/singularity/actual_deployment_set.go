package singularity

import (
	"fmt"
	"sync"
	"time"

	"github.com/opentable/go-singularity/dtos"
	"github.com/opentable/sous/lib"
	"github.com/opentable/sous/util/logging"
	"github.com/opentable/sous/util/logging/messages"
	"github.com/pkg/errors"
)

const (
	// MaxAssemblers is the maximum number of simultaneous deployment
	// assemblers.
	MaxAssemblers = 100
)

type (
	sHistory   *dtos.SingularityDeployHistory
	sDeploy    *dtos.SingularityDeploy
	sRequest   *dtos.SingularityRequest
	sDepMarker *dtos.SingularityDeployMarker

	// SingReq captures a request made to singularity with its initial response
	SingReq struct {
		SourceURL string
		Sing      singClient
		ReqParent *dtos.SingularityRequestParent
	}

	retryCounter struct {
		count map[string]uint
		log   logging.LogSink
	}
)

// RunningDeployments collects data from the Singularity clusters and
// returns a list of actual deployments.
func (sc *deployer) RunningDeployments(reg sous.Registry, clusters sous.Clusters) (sous.DeployStates, error) {
	var deps sous.DeployStates
	retries := retryCounter{
		count: map[string]uint{},
		log:   sc.log,
	}
	errCh := make(chan error)
	deps = sous.NewDeployStates()
	sings := make(map[string]struct{})
	reqCh := make(chan SingReq, len(clusters)*sc.ReqsPerServer)
	depCh := make(chan *sous.DeployState, sc.ReqsPerServer)

	defer close(depCh)
	// XXX The intention here was to use something like the gotools context to
	// manage NW cancellation
	//defer sc.rectClient.Cancel()

	var depAssWait, singWait, depWait sync.WaitGroup
	messages.ReportLogFieldsMessage("Setting up to wait for clusters", logging.ExtraDebug1Level, sc.log, clusters)
	singWait.Add(len(clusters))
	for _, url := range clusters {
		url := url.BaseURL
		if _, ok := sings[url]; ok {
			singWait.Done()
			continue
		}
		//sing.Debug = true
		sings[url] = struct{}{}
		client := sc.buildSingClient(url)
		go sc.singPipeline(reg, url, client, &depWait, &singWait, reqCh, errCh, clusters)
	}

	go sc.depPipeline(reg, clusters, MaxAssemblers, &depAssWait, reqCh, depCh, errCh)

	go func() {
		defer catchAndSend("closing channels", errCh, sc.log)

		singWait.Wait()
		messages.ReportLogFieldsMessage("All singularities polled for requests", logging.DebugLevel, sc.log)

		depWait.Wait()
		messages.ReportLogFieldsMessage("All deploys processed", logging.DebugLevel, sc.log)

		depAssWait.Wait()
		messages.ReportLogFieldsMessage("All deployments assembled", logging.DebugLevel, sc.log)

		close(reqCh)
		messages.ReportLogFieldsMessage("Closed reqCh", logging.DebugLevel, sc.log)

		close(errCh)
		messages.ReportLogFieldsMessage("Closed errCh", logging.DebugLevel, sc.log)
	}()

	for {
		select {
		case dep := <-depCh:
			deps.Add(dep)
			messages.ReportLogFieldsMessage("Adding deployment", logging.DebugLevel, sc.log, dep, deps.Len())
			depWait.Done()
		case err, cont := <-errCh:
			if !cont {
				messages.ReportLogFieldsMessage("Errors channel closed. Finishing up.", logging.DebugLevel, sc.log)
				return deps, nil
			}
			if isMalformed(err) || ignorableDeploy(sc.log, err) {
				if isMalformed(err) {
					logging.ReportError(sc.log, errors.Wrapf(err, "malformed"))
				} else {
					messages.ReportLogFieldsMessage("Ignorable deploy.", logging.DebugLevel, sc.log)
				}
				depWait.Done()
			} else {
				retryable := retries.maybe(err, reqCh)
				if !retryable {
					logging.ReportError(sc.log, errors.Wrapf(err, "cannot retry"))
					return deps, err
				}
			}
		}
	}
}

const retryLimit = 3

func (rc retryCounter) maybe(err error, reqCh chan SingReq) bool {
	rt, ok := errors.Cause(err).(*canRetryRequest)
	if !ok {
		return false
	}
	count, ok := rc.count[rt.name()]
	if !ok {
		count = 0
	}
	if count > retryLimit {
		return false
	}

	rc.count[rt.name()] = count + 1
	go func() {
		defer catchAll("retrying: "+rt.req.SourceURL, rc.log)
		time.Sleep(time.Millisecond * 50)
		reqCh <- rt.req
	}()

	return true
}

func catchAll(from string, log logging.LogSink) {
	if err := recover(); err != nil {
		if e, is := err.(error); is {
			logging.ReportError(log, errors.Wrapf(e, "Recovering from panic: %s", from))
		} else {
			messages.ReportLogFieldsMessage("Panicked with non-error", logging.DebugLevel, log, err, from)
		}
	}
}

func dontrecover() error {
	return nil
}

func catchAndSend(from string, errs chan error, log logging.LogSink) {
	defer catchAll(from, log)
	if err := recover(); err != nil {
		if e, is := err.(error); is {
			logging.ReportError(log, errors.Wrapf(e, "Recovering from panic: %s", from))
		} else {
			messages.ReportLogFieldsMessage("Panicked with non-error", logging.DebugLevel, log, err, from)
		}
		switch err := err.(type) {
		default:
			if err != nil {
				errs <- fmt.Errorf("%s: Panicked with not-error: %v", from, err)
			}
		case error:
			errs <- errors.Wrapf(err, from)
		}
	}
}

func (sc *deployer) singPipeline(
	reg sous.Registry,
	url string,
	client singClient,
	dw, wg *sync.WaitGroup,
	reqs chan SingReq,
	errs chan error,
	clusters sous.Clusters,
) {
	messages.ReportLogFieldsMessage("Starting Cluster", logging.ExtraDebug1Level, sc.log, url)
	defer func() { messages.ReportLogFieldsMessage("Completed Cluster", logging.ExtraDebug1Level, sc.log, url) }()
	defer wg.Done()
	defer catchAndSend(fmt.Sprintf("get requests: %s", url), errs, sc.log)
	srp, err := sc.getSingularityRequestParents(client)
	if err != nil {
		messages.ReportLogFieldsMessage("Error in singPipeline", logging.ExtraDebug1Level, sc.log, err)
		errs <- errors.Wrap(err, "getting request list")
		return
	}

	rs := convertSingularityRequestParentsToSingReqs(url, client, srp)

	for _, r := range rs {
		messages.ReportLogFieldsMessage("Request",
			logging.ExtraDebug1Level,
			sc.log, r.SourceURL,
			reqID(r.ReqParent),
			r.ReqParent.Request.Instances)
		dw.Add(1)
		reqs <- r
	}
}

func (sc *deployer) getSingularityRequestParents(client singClient) (dtos.SingularityRequestParentList, error) {
	singRequests, err := client.GetRequests(false) // = don't use the 30 second cache

	return singRequests, errors.Wrap(err, "getting request")
}

func convertSingularityRequestParentsToSingReqs(url string, client singClient, srp dtos.SingularityRequestParentList) []SingReq {
	reqs := make([]SingReq, 0, len(srp))

	for _, sr := range srp {
		reqs = append(reqs, SingReq{url, client, sr})
	}
	return reqs
}

func (sc *deployer) depPipeline(
	reg sous.Registry,
	clusters sous.Clusters,
	poolCount int,
	depAssWait *sync.WaitGroup,
	reqCh chan SingReq,
	depCh chan *sous.DeployState,
	errCh chan error,
) {
	defer catchAndSend("dependency building", errCh, sc.log)
	// XXX This doesn't as yet actually restrict the number of assemblers.
	poolLimit := make(chan struct{}, poolCount)
	for req := range reqCh {
		depAssWait.Add(1)
		messages.ReportLogFieldsMessage("started assembling", logging.ExtraDebug1Level, sc.log, reqID(req.ReqParent))
		go func(req SingReq) {
			defer depAssWait.Done()
			defer catchAndSend(fmt.Sprintf("dep from req %s", req.SourceURL), errCh, sc.log)
			poolLimit <- struct{}{}
			defer func() {
				messages.ReportLogFieldsMessage("finished assembling", logging.ExtraDebug1Level, sc.log, reqID(req.ReqParent))
				<-poolLimit
			}()

			dep, err := sc.assembleDeployState(reg, clusters, req)

			if err != nil {
				errCh <- errors.Wrap(err, "assembly problem")
			} else {
				depCh <- dep
			}
		}(req)
	}
}

func (sc *deployer) assembleDeployState(reg sous.Registry, clusters sous.Clusters, req SingReq) (*sous.DeployState, error) {
	messages.ReportLogFieldsMessage("Assembling deploy state", logging.ExtraDebug1Level, sc.log, req.SourceURL, reqID(req.ReqParent))
	tgt, err := BuildDeployment(reg, clusters, req, sc.log)
	messages.ReportLogFieldsMessage("Collected deployment", logging.ExtraDebug1Level, sc.log, tgt)
	return &tgt, errors.Wrap(err, "Building deployment")
}
