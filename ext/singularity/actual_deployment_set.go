package singularity

import (
	"fmt"
	"io/ioutil"
	"os"
	"runtime/debug"
	"sync"
	"time"

	"github.com/opentable/go-singularity"
	"github.com/opentable/go-singularity/dtos"
	"github.com/opentable/sous/lib"
	"github.com/pkg/errors"
)

// ReqsPerServer limits the number of simultaneous number of requests made
// against a single Singularity server
const (
	ReqsPerServer = 10
	MaxAssemblers = 100
)

type (
	sHistory   *dtos.SingularityDeployHistory
	sDeploy    *dtos.SingularityDeploy
	sRequest   *dtos.SingularityRequest
	sDepMarker *dtos.SingularityDeployMarker

	// A SingClient provides the interface needed to assemble a deployment.
	SingClient interface {
		GetDeploy(requestID string, deployID string) (*dtos.SingularityDeployHistory, error)
		GetDeploys(requestID string, count, page int32) (dtos.SingularityDeployHistoryList, error)
	}

	// SingReq captures a request made to singularity with its initial response
	SingReq struct {
		SourceURL string
		Sing      SingClient
		ReqParent *dtos.SingularityRequestParent
	}

	retryCounter map[string]uint
)

// RunningDeployments collects data from the Singularity clusters and
// returns a list of actual deployments.
func (sc *deployer) RunningDeployments(reg sous.Registry, clusters sous.Clusters) (sous.DeployStates, error) {
	var deps sous.DeployStates
	retries := make(retryCounter)
	errCh := make(chan error)
	deps = sous.NewDeployStates()
	reqCh := make(chan SingReq, len(clusters)*ReqsPerServer)
	depCh := make(chan *sous.DeployState, ReqsPerServer)

	defer close(depCh)
	// XXX The intention here was to use something like the gotools context to
	// manage NW cancellation
	//defer sc.rectClient.Cancel()

	var singWait, depWait sync.WaitGroup

	Log.Vomit.Printf("Setting up to wait for %d clusters", len(clusters))
	singWait.Add(len(clusters))
	for _, url := range clusters {
		url := url.BaseURL
		client := sc.buildSingClient(url)
		go singPipeline(reg, url, client, &depWait, &singWait, reqCh, errCh, clusters)
	}

	go depPipeline(reg, clusters, MaxAssemblers, reqCh, depCh, errCh)

	go func() {
		catchAndSend("closing up", errCh)
		singWait.Wait()
		Log.Debug.Println("All singularities polled for requests")
		depWait.Wait()
		Log.Debug.Println("All deploys processed")

		close(reqCh)
		close(errCh)
	}()

	for {
		select {
		case dep := <-depCh:
			deps.Add(dep)
			Log.Debug.Printf("Deployment #%d: %+v", deps.Len(), dep)
			depWait.Done()
		case err, cont := <-errCh:
			if !cont {
				Log.Debug.Printf("Errors channel closed. Finishing up.")
				return deps, nil
			}
			if isMalformed(err) || ignorableDeploy(err) {
				Log.Debug.Print(err)
				depWait.Done()
			} else {
				retryable := retries.maybe(err, reqCh)
				if !retryable {
					Log.Notice.Printf("Cannot retry: %v. Exiting", err)
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

	Log.Debug.Printf("%T err = %+v\n", errors.Cause(err), errors.Cause(err))
	count, ok := rc[rt.name()]
	if !ok {
		count = 0
	}
	if count > retryLimit {
		return false
	}

	rc[rt.name()] = count + 1
	go func() {
		defer catchAll("retrying: " + rt.req.SourceURL)
		time.Sleep(time.Millisecond * 50)
		reqCh <- rt.req
	}()

	return true
}

func catchAll(from string) {
	if err := recover(); err != nil {
		Log.Warn.Printf("Recovering from %s where we received %v", from, err)
	}
}

func dontrecover() error {
	return nil
}

func catchAndSend(from string, errs chan error) {
	defer catchAll(from)
	if err := recover(); err != nil {
		Log.Debug.Printf("from = %s err = %+v\n", from, err)
		Log.Debug.Printf("debug.Stack() = %+v\n", string(debug.Stack()))
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

func logFDs(when string) {
	defer func() { recover() }() // this is just diagnostic
	pid := os.Getpid()
	fdDir, err := ioutil.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
	if err != nil {
		Log.Vomit.Print(err)
		return
	}
	/*
		for _, f := range fdDir {
			n, e := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", pid, f.Name()))
			if e != nil {
				n = f.Name()
			}

			Log.Vomit.Printf("%s: %s", f.Mode(), n)
		}
	*/

	Log.Debug.Printf("%s: %d", when, len(fdDir))
}

func singPipeline(
	reg sous.Registry,
	url string,
	client *singularity.Client,
	dw, wg *sync.WaitGroup,
	reqs chan SingReq,
	errs chan error,
	//	clusters []string,
	clusters sous.Clusters,
) {
	Log.Vomit.Printf("Starting cluster at %s", url)
	defer func() { Log.Vomit.Printf("Completed cluster at %s", url) }()
	defer wg.Done()
	defer catchAndSend(fmt.Sprintf("get requests: %s", url), errs)
	srp, err := getSingularityRequestParents(client)
	if err != nil {
		Log.Vomit.Print(err) //XXX connection reset by peer should be retried
		errs <- errors.Wrap(err, "getting request list")
		return
	}

	rs := convertSingularityRequestParentsToSingReqs(url, client, srp)

	for _, r := range rs {
		Log.Vomit.Printf("Req: %s %s %d", r.SourceURL, reqID(r.ReqParent), r.ReqParent.Request.Instances)
		dw.Add(1)
		reqs <- r
	}
}

func getSingularityRequestParents(client *singularity.Client) ([]*dtos.SingularityRequestParent, error) {
	logFDs("before getRequestsFromSingularity")
	defer logFDs("after getRequestsFromSingularity")
	singRequests, err := client.GetRequests()

	return singRequests, errors.Wrap(err, "getting request")
}

func convertSingularityRequestParentsToSingReqs(url string, client *singularity.Client, srp []*dtos.SingularityRequestParent) []SingReq {
	reqs := make([]SingReq, 0, len(srp))

	for _, sr := range srp {
		reqs = append(reqs, SingReq{url, client, sr})
	}
	return reqs
}

func depPipeline(
	reg sous.Registry,
	clusters sous.Clusters,
	poolCount int,
	reqCh chan SingReq,
	depCh chan *sous.DeployState,
	errCh chan error,
) {
	defer catchAndSend("dependency building", errCh)
	poolLimit := make(chan struct{}, poolCount)
	for req := range reqCh {
		Log.Vomit.Printf("starting assembling for %q", reqID(req.ReqParent))
		go func(req SingReq) {
			defer catchAndSend(fmt.Sprintf("dep from req %s", req.SourceURL), errCh)

			poolLimit <- struct{}{}
			defer func() {
				Log.Vomit.Printf("finished assembling for %q", reqID(req.ReqParent))
				<-poolLimit
			}()

			dep, err := assembleDeployState(reg, clusters, req)

			if err != nil {
				errCh <- errors.Wrap(err, "assembly problem")
			} else {
				depCh <- dep
			}
		}(req)
	}
}

func assembleDeployState(reg sous.Registry, clusters sous.Clusters, req SingReq) (*sous.DeployState, error) {
	Log.Vomit.Printf("Assembling from: %s %s", req.SourceURL, reqID(req.ReqParent))
	tgt, err := BuildDeployment(reg, clusters, req)
	Log.Vomit.Printf("Collected deployment: %#v", tgt)
	return &tgt, errors.Wrap(err, "Building deployment")
}
