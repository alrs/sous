package singularity

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/opentable/go-singularity"
	"github.com/opentable/sous/lib"
	"github.com/opentable/sous/util/logging"
	"github.com/opentable/swaggering"
	"github.com/pkg/errors"
	"github.com/satori/go.uuid"
)

// Both of these values are (for reasons only known to the spirits)
// _configurable_ in singularity. If you've done something silly like configure
// them differently than their defaults, at the moment we wish you the best of
// luck, and vaya con Dios.
// c.f. https://github.com/HubSpot/Singularity/blob/master/Docs/reference/configuration.md#limits

// Singularity DeployID must be <50
const maxDeployIDLen = 49

// Singularity RequestID must be <100
const maxRequestIDLen = 99

// maxVersionLen needs to account for the separator character
// between the version string and the UUID string.
const maxVersionLen = 31

// DefaultMaxHTTPConcurrencyPerServer is the default maximum number of
// concurrent HTTP requests to send per Singularity server.
// To configure per deployer, see OptMaxHTTPConcurrencyPerServer.
const DefaultMaxHTTPConcurrencyPerServer = 10

/*
The imagined use case here is like this:

intendedSet := getFromManifests()
existingSet := getFromSingularity()

dChans := intendedSet.Diff(existingSet)
*/

type (
	deployer struct {
		Client        rectificationClient
		singFac       func(string) *singularity.Client
		ReqsPerServer int
		log           logging.LogSink
	}

	// DeployerOption is an option for configuring singularity deployers.
	DeployerOption func(*deployer)

	// rectificationClient abstracts the raw interactions with Singularity.
	rectificationClient interface {
		// Deploy creates a new deploy on a particular requeust
		Deploy(d sous.Deployable, reqID string) error

		// PostRequest sends a request to a Singularity cluster to initiate
		PostRequest(d sous.Deployable, reqID string) error

		// DeleteRequest instructs Singularity to delete a particular request
		DeleteRequest(cluster, reqID, message string) error
	}

	// DTOMap is shorthand for map[string]interface{}
	dtoMap map[string]interface{}
)

func sanitizeDeployID(in string) string {
	return illegalDeployIDChars.ReplaceAllString(in, "_")
}

func stripDeployID(in string) string {
	return illegalDeployIDChars.ReplaceAllString(in, "")
}

// NewDeployer creates a new Singularity-based sous.Deployer.
func NewDeployer(c rectificationClient, ls logging.LogSink, options ...DeployerOption) sous.Deployer {
	d := &deployer{Client: c, log: ls, ReqsPerServer: DefaultMaxHTTPConcurrencyPerServer}
	for _, opt := range options {
		opt(d)
	}
	return d
}

// OptMaxHTTPReqsPerServer overrides the DefaultMaxHTTPConcurrencyPerServer
// for this server.
func OptMaxHTTPReqsPerServer(n int) DeployerOption {
	return func(d *deployer) { d.ReqsPerServer = n }
}

// Rectify invokes actions to ensure that the real world matches pair.Post,
// given that it currently matches pair.Prior.
func (r *deployer) Rectify(pair *sous.DeployablePair) sous.DiffResolution {
	switch k := pair.Kind(); k {
	default:
		panic(fmt.Sprintf("unrecognised kind %q", k))
	case sous.SameKind:
		resolution := pair.SameResolution()
		if pair.Post.Status == sous.DeployStatusFailed {
			resolution.Error = sous.WrapResolveError(&sous.FailedStatusError{})
		}
		return resolution
	case sous.AddedKind:
		result := sous.DiffResolution{DeploymentID: pair.ID()}
		if err := r.RectifySingleCreate(pair); err != nil {
			result.Desc = "not created"
			switch t := err.(type) {
			default:
				result.Error = sous.WrapResolveError(&sous.CreateError{Deployment: pair.Post.Deployment.Clone(), Err: err})
			case *swaggering.ReqError:
				if t.Status == 400 {
					result.Error = sous.WrapResolveError(err)
				} else {
					result.Error = sous.WrapResolveError(&sous.CreateError{Deployment: pair.Post.Deployment.Clone(), Err: err})
				}
			}
		} else {
			result.Desc = sous.CreateDiff
		}
		Log.Vomit.Printf("Reporting result of create: %#v", result)
		return result
	case sous.RemovedKind:
		result := sous.DiffResolution{DeploymentID: pair.ID()}
		if err := r.RectifySingleDelete(pair); err != nil {
			result.Error = sous.WrapResolveError(&sous.DeleteError{Deployment: pair.Prior.Deployment.Clone(), Err: err})
			result.Desc = "not deleted"
		} else {
			result.Desc = sous.DeleteDiff
		}
		Log.Vomit.Printf("Reporting result of delete: %#v", result)
		return result
	case sous.ModifiedKind:
		result := sous.DiffResolution{DeploymentID: pair.ID()}
		if err := r.RectifySingleModification(pair); err != nil {
			dp := &sous.DeploymentPair{
				Prior: pair.Prior.Deployment.Clone(),
				Post:  pair.Post.Deployment.Clone(),
			}
			Log.Debug.Print(err)
			result.Error = sous.WrapResolveError(&sous.ChangeError{Deployments: dp, Err: err})
			result.Desc = "not updated"
		} else if pair.Prior.Status == sous.DeployStatusFailed || pair.Post.Status == sous.DeployStatusFailed {
			result.Desc = sous.ModifyDiff
			result.Error = sous.WrapResolveError(&sous.FailedStatusError{})
		} else {
			result.Desc = sous.ModifyDiff
		}
		Log.Vomit.Printf("Reporting result of modify: %#v", result)
		return result
	}
}

func (r *deployer) SetSingularityFactory(fn func(string) *singularity.Client) {
	r.singFac = fn
}

func (r *deployer) buildSingClient(url string) *singularity.Client {
	if r.singFac == nil {
		return singularity.NewClient(url, r.log)
	}
	return r.singFac(url)
}

func rectifyRecover(d interface{}, f string, err *error) {
	if r := recover(); r != nil {
		stack := string(debug.Stack())
		logging.Log.Warn.Printf("Panic in %s with %# v", f, d)
		logging.Log.Warn.Printf("  %v", r)
		logging.Log.Warn.Print(stack)
		*err = errors.Errorf("Panicked: %s; stack trace:\n%s", r, stack)
	}
}

func (r *deployer) RectifySingleCreate(d *sous.DeployablePair) (err error) {
	Log.Debug.Printf("Rectifying creation %q:  \n %# v", d.ID(), d.Post)
	defer rectifyRecover(d, "RectifySingleCreate", &err)
	if err != nil {
		return err
	}
	reqID, err := computeRequestID(d.Post)
	if err != nil {
		return err
	}
	if err = r.Client.PostRequest(*d.Post, reqID); err != nil {
		return err
	}
	return r.Client.Deploy(*d.Post, reqID)
}

func (r *deployer) RectifySingleDelete(d *sous.DeployablePair) (err error) {
	defer rectifyRecover(d, "RectifySingleDelete", &err)
	data, ok := d.ExecutorData.(*singularityTaskData)
	if !ok {
		return errors.Errorf("Delete record %#v doesn't contain Singularity compatible data: was %T\n\t%#v", d.ID(), data, d)
	}
	requestID := data.requestID

	// TODO: Alert the owner of this request that there is no manifest for it;
	// they should either delete the request manually, or else add the manifest back.
	logging.Log.Warn.Printf("NOT DELETING REQUEST %q (FOR: %q)", requestID, d.ID())
	return nil
	// The following line deletes requests when it is not commented out.
	//return r.Client.DeleteRequest(d.Cluster.BaseURL, requestID, "deleting request for removed manifest")
}

func (r *deployer) RectifySingleModification(pair *sous.DeployablePair) (err error) {
	different, _ := pair.Post.Deployment.Diff(pair.Prior.Deployment) //TODO LH we will want to log the diffs (second param here)
	if !different {
		reportDeployerMessage("Attempting to rectify empty diff",
			pair, nil, nil, logging.WarningLevel, logging.Log)
	}

	reportDeployerMessage("Rectifying modified diffs", pair, nil, nil, logging.InformationLevel, logging.Log)

	defer rectifyRecover(pair, "RectifySingleModification", &err)

	data, ok := pair.ExecutorData.(*singularityTaskData)
	if !ok {
		err := errors.Errorf("Modification record %#v doesn't contain Singularity compatible data: was %T\n\t%#v", pair.ID(), data, pair)
		reportDeployerMessage("Error modification not compatible with Singularity", pair, nil, err, logging.WarningLevel, logging.Log)
		return err
	}
	reqID := data.requestID

	changesApplied := false
	reportDeployerMessage("Operating on request", pair, data, nil, logging.ExtraDebug1Level, logging.Log)
	if changesReq(pair) {
		reportDeployerMessage("Updating request", pair, data, nil, logging.DebugLevel, logging.Log)
		if err := r.Client.PostRequest(*pair.Post, reqID); err != nil {
			reportDeployerMessage("Error posting request to Singularity", pair, data, err, logging.WarningLevel, logging.Log)
			return err
		}
		changesApplied = true
	} else {
		reportDeployerMessage("No change to request", pair, data, nil, logging.DebugLevel, logging.Log)
	}

	if changesDep(pair) {
		reportDeployerMessage("Deploying", pair, data, nil, logging.DebugLevel, logging.Log)
		if err := r.Client.Deploy(*pair.Post, reqID); err != nil {
			reportDeployerMessage(err.Error(), pair, data, nil, logging.WarningLevel, logging.Log)
			return err
		}
		changesApplied = true
	} else {
		reportDeployerMessage("No change to deploy for request", pair, data, nil, logging.DebugLevel, logging.Log)
	}

	return nil
}

// XXX for logging and other UI purposes, the best thing would be if the
// DeployablePair had a "diff" method that returned a (cached) list of
// differences, which these two functions could filter for req/dep triggering
// changes. Then, rather than simply computing the conditional, the deployer
// could report ("deploy required because of %v", diffs)

func changesReq(pair *sous.DeployablePair) bool {
	return (pair.Prior.Kind == sous.ManifestKindScheduled && pair.Prior.Schedule != pair.Post.Schedule) ||
		pair.Prior.Kind != pair.Post.Kind ||
		pair.Prior.NumInstances != pair.Post.NumInstances ||
		!pair.Prior.Owners.Equal(pair.Post.Owners)
}

func changesDep(pair *sous.DeployablePair) bool {
	return pair.Post.Status == sous.DeployStatusFailed ||
		pair.Prior.Status == sous.DeployStatusFailed ||
		!(pair.Prior.SourceID.Equal(pair.Post.SourceID) &&
			pair.Prior.Resources.Equal(pair.Post.Resources) &&
			pair.Prior.Env.Equal(pair.Post.Env) &&
			pair.Prior.DeployConfig.Volumes.Equal(pair.Post.DeployConfig.Volumes) &&
			pair.Prior.Startup.Equal(pair.Post.Startup))
}

func computeRequestID(d *sous.Deployable) (string, error) {
	return MakeRequestID(d.ID())
}

// MakeRequestID creates a Singularity request ID from a sous.DeploymentID.
func MakeRequestID(depID sous.DeploymentID) (string, error) {
	sn, err := depID.ManifestID.Source.ShortName()
	if err != nil {
		return "", err
	}
	sn = illegalDeployIDChars.ReplaceAllString(sn, "_")
	dd := illegalDeployIDChars.ReplaceAllString(depID.ManifestID.Source.Dir, "_")
	fl := illegalDeployIDChars.ReplaceAllString(depID.ManifestID.Flavor, "_")
	cl := illegalDeployIDChars.ReplaceAllString(depID.Cluster, "_")
	digest := depID.Digest()

	reqBase := fmt.Sprintf("%s-%s-%s-%s-%x", sn, dd, fl, cl, digest)

	if len(reqBase) > maxRequestIDLen {
		return reqBase[:(maxRequestIDLen)], nil
	}
	return reqBase, nil
}

func computeDeployID(d *sous.Deployable) string {
	var versionTrunc string
	uuidEntire := stripDeployID(uuid.NewV4().String())
	versionSansMeta := stripMetadata(d.Deployment.SourceID.Version.String())
	versionEntire := sanitizeDeployID(versionSansMeta)

	if len(versionEntire) > maxVersionLen {
		versionTrunc = versionEntire[0:maxVersionLen]
	} else {
		versionTrunc = versionEntire
	}

	depBase := strings.Join([]string{
		versionTrunc,
		uuidEntire,
	}, "_")

	if len(depBase) > maxDeployIDLen {
		return depBase[:(maxDeployIDLen)]
	}
	return depBase
}
