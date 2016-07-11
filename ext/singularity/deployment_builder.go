package singularity

import (
	"fmt"

	"github.com/opentable/go-singularity/dtos"
	"github.com/opentable/sous/ext/docker"
	"github.com/opentable/sous/lib"
)

type (
	deploymentBuilder struct {
		nicks         map[string]string
		Target        sous.Deployment
		depMarker     sDepMarker
		deploy        sDeploy
		request       sRequest
		req           SingReq
		rectification rectificationClient
	}

	canRetryRequest struct {
		cause error
		req   SingReq
	}

	malformedResponse struct {
		message string
	}
)

func (mr malformedResponse) Error() string {
	return mr.message
}

func (cr *canRetryRequest) Error() string {
	return fmt.Sprintf("%s: %s", cr.cause, cr.name())
}

func (cr *canRetryRequest) name() string {
	return fmt.Sprintf("%s:%s", cr.req.SourceURL, cr.req.ReqParent.Request.Id)
}

// NewDeploymentBuilder creates a deploymentBuilder prepared to collect the
// data associated with req and return a Deployment
// XXX
func NewDeploymentBuilder(cl rectificationClient, nicks map[string]string, req SingReq) (deploymentBuilder, error) {
	db := deploymentBuilder{rectification: cl, nicks: nicks, req: req}

	db.Target.Cluster = req.SourceURL
	db.request = req.ReqParent.Request

	return db, db.completeConstruction()
}

func (db *deploymentBuilder) canRetry(err error) error {
	if _, ok := err.(malformedResponse); ok {
		return err
	}

	if db.req.SourceURL == "" {
		return err
	}

	if db.req.ReqParent == nil {
		return err
	}
	if db.req.ReqParent.Request == nil {
		return err
	}

	if db.req.ReqParent.Request.Id == "" {
		return err
	}

	return &canRetryRequest{err, db.req}
}

func (db *deploymentBuilder) completeConstruction() error {
	if err := db.retrieveDeploy(); err != nil {
		return db.canRetry(err)
	}
	if err := db.retrieveImageLabels(); err != nil {
		return db.canRetry(err)
	}
	if err := db.unpackDeployConfig(); err != nil {
		return db.canRetry(err)
	}
	if err := db.determineManifestKind(); err != nil {
		return db.canRetry(err)
	}

	return nil
}

func (db *deploymentBuilder) retrieveDeploy() error {

	rp := db.req.ReqParent
	rds := rp.RequestDeployState
	sing := db.req.Sing

	if rds == nil {
		return malformedResponse{"Singularity response didn't include a deploy state. ReqId: " + rp.Request.Id}
	}
	db.depMarker = rds.PendingDeploy
	if db.depMarker == nil {
		db.depMarker = rds.ActiveDeploy
	}
	if db.depMarker == nil {
		return malformedResponse{"Singularity deploy state included no dep markers. ReqID: " + rp.Request.Id}
	}

	// !!! makes HTTP req
	dh, err := sing.GetDeploy(db.depMarker.RequestId, db.depMarker.DeployId)
	if err != nil {
		return err
	}

	db.deploy = dh.Deploy
	if db.deploy == nil {
		return malformedResponse{"Singularity deploy history included no deploy"}
	}

	return nil
}

func (db *deploymentBuilder) retrieveImageLabels() error {
	ci := db.deploy.ContainerInfo
	if ci.Type != dtos.SingularityContainerInfoSingularityContainerTypeDOCKER {
		return fmt.Errorf("Singularity container isn't a docker container")
	}
	dkr := ci.Docker
	if dkr == nil {
		return malformedResponse{"Singularity deploy didn't include a docker info"}
	}

	imageName := dkr.Image

	// XXX coupled to Docker registry as ImageMapper
	// !!! HTTP request
	labels, err := db.rectification.ImageLabels(imageName)
	if err != nil {
		return malformedResponse{err.Error()}
	}
	Log.Vomit.Print("Labels: ", labels)

	db.Target.SourceVersion, err = docker.SourceVersionFromLabels(labels)
	if err != nil {
		return malformedResponse{fmt.Sprintf("For reqID: %s, %s", db.req.ReqParent.Request.Id, err.Error())}
	}

	var posNick string
	matchCount := 0
	for nn, url := range db.nicks {
		if url != db.req.SourceURL {
			continue
		}
		posNick = nn
		matchCount++

		checkID := buildReqID(db.Target.SourceVersion, nn)
		sous.Log.Vomit.Printf("Trying hypothetical request ID: %s", checkID)
		if checkID == db.request.Id {
			db.Target.ClusterNickname = nn
			sous.Log.Debug.Printf("Found cluster: %s", nn)
			break
		}
	}
	if db.Target.ClusterNickname == "" {
		if matchCount == 1 {
			db.Target.ClusterNickname = posNick
			return nil
		}
		sous.Log.Debug.Printf("No cluster nickname (%#v) matched request id %s for %s", db.nicks, db.request.Id, imageName)
		return malformedResponse{fmt.Sprintf("No cluster nickname (%#v) matched request id %s", db.nicks, db.request.Id)}
	}

	return nil
}

func (db *deploymentBuilder) unpackDeployConfig() error {
	db.Target.Env = db.deploy.Env
	Log.Vomit.Printf("Env %+v", db.deploy.Env)
	if db.Target.Env == nil {
		db.Target.Env = make(map[string]string)
	}

	singRez := db.deploy.Resources
	db.Target.Resources = make(sous.Resources)
	db.Target.Resources["cpus"] = fmt.Sprintf("%f", singRez.Cpus)
	db.Target.Resources["memory"] = fmt.Sprintf("%f", singRez.MemoryMb)
	db.Target.Resources["ports"] = fmt.Sprintf("%d", singRez.NumPorts)

	db.Target.NumInstances = int(db.request.Instances)
	db.Target.Owners = make(sous.OwnerSet)
	for _, o := range db.request.Owners {
		db.Target.Owners.Add(o)
	}

	for _, v := range db.deploy.ContainerInfo.Volumes {
		db.Target.DeployConfig.Volumes = append(db.Target.DeployConfig.Volumes,
			&sous.Volume{
				Host:      v.HostPath,
				Container: v.ContainerPath,
				Mode:      sous.VolumeMode(v.Mode),
			})
	}
	Log.Vomit.Printf("Volumes %+v", db.Target.DeployConfig.Volumes)
	if len(db.Target.DeployConfig.Volumes) > 0 {
		Log.Debug.Printf("%+v", db.Target.DeployConfig.Volumes[0])
	}

	return nil
}

func (db *deploymentBuilder) determineManifestKind() error {
	switch db.request.RequestType {
	default:
		return fmt.Errorf("Unrecognized response type returned by Singularity: %v", db.request.RequestType)
	case dtos.SingularityRequestRequestTypeSERVICE:
		db.Target.Kind = sous.ManifestKindService
	case dtos.SingularityRequestRequestTypeWORKER:
		db.Target.Kind = sous.ManifestKindWorker
	case dtos.SingularityRequestRequestTypeON_DEMAND:
		db.Target.Kind = sous.ManifestKindOnDemand
	case dtos.SingularityRequestRequestTypeSCHEDULED:
		db.Target.Kind = sous.ManifestKindScheduled
	case dtos.SingularityRequestRequestTypeRUN_ONCE:
		db.Target.Kind = sous.ManifestKindOnce
	}
	return nil
}