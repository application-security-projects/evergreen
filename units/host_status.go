package units

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/evergreen-ci/evergreen"
	"github.com/evergreen-ci/evergreen/cloud"
	"github.com/evergreen-ci/evergreen/model/distro"
	"github.com/evergreen-ci/evergreen/model/host"
	"github.com/mongodb/amboy"
	"github.com/mongodb/amboy/dependency"
	"github.com/mongodb/amboy/job"
	"github.com/mongodb/amboy/registry"
	"github.com/mongodb/grip"
	"github.com/mongodb/grip/message"
	"github.com/pkg/errors"
)

const cloudHostReadyJobName = "set-cloud-hosts-ready"

func init() {
	registry.AddJobType(cloudHostReadyJobName,
		func() amboy.Job { return makeCloudHostReadyJob() })
}

type cloudHostReadyJob struct {
	job.Base `bson:"job_base" json:"job_base" yaml:"job_base"`
	env      evergreen.Environment
}

// NewCloudHostReadyJob gets statuses for all jobs created by Cloud providers which the Cloud providers'
// APIs have not yet returned all running. It marks the hosts running in the database.
func NewCloudHostReadyJob(env evergreen.Environment, id string) amboy.Job {
	j := makeCloudHostReadyJob()
	j.SetID(fmt.Sprintf("%s.%s", cloudHostReadyJobName, id))
	j.env = env
	j.SetPriority(1)
	j.SetScopes([]string{cloudHostReadyJobName})
	// Jobs never appear to exceed 1 minute, but add a bunch of padding.
	j.UpdateTimeInfo(amboy.JobTimeInfo{MaxTime: 10 * time.Minute})
	return j
}

func makeCloudHostReadyJob() *cloudHostReadyJob {
	j := &cloudHostReadyJob{
		Base: job.Base{
			JobType: amboy.JobType{
				Name:    cloudHostReadyJobName,
				Version: 1,
			},
		},
	}

	j.SetDependency(dependency.NewAlways())
	return j
}

func (j *cloudHostReadyJob) Run(ctx context.Context) {
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)
	defer cancel()
	defer j.MarkComplete()
	if j.env == nil {
		j.env = evergreen.GetEnvironment()
	}

	// Collect hosts by provider and region
	settings, err := evergreen.GetConfig()
	if err != nil {
		j.AddError(errors.Wrap(err, "unable to get evergreen settings"))
		return
	}
	startingHostsByClient, err := host.StartingHostsByClient(settings.HostInit.CloudStatusBatchSize)
	if err != nil {
		j.AddError(errors.Wrap(err, "can't get starting hosts"))
		return
	}
clientsLoop:
	for clientOpts, hosts := range startingHostsByClient {
		if ctx.Err() != nil {
			j.AddError(ctx.Err())
			return
		}
		if len(hosts) == 0 {
			continue
		}
		mgrOpts := cloud.ManagerOpts{
			Provider:       clientOpts.Provider,
			Region:         clientOpts.Region,
			ProviderKey:    clientOpts.Key,
			ProviderSecret: clientOpts.Secret,
		}
		m, err := cloud.GetManager(ctx, j.env, mgrOpts)
		if err != nil {
			j.AddError(errors.Wrap(err, "error getting cloud manager"))
			return
		}
		if batch, ok := m.(cloud.BatchManager); ok {
			statuses, err := batch.GetInstanceStatuses(ctx, hosts)
			if err != nil {
				if strings.Contains(err.Error(), "InvalidInstanceID.NotFound") {
					j.AddError(j.terminateUnknownHosts(ctx, err.Error()))
					continue clientsLoop
				}
				j.AddError(errors.Wrap(err, "error getting host statuses for providers"))
				continue clientsLoop
			}
			if len(statuses) != len(hosts) {
				j.AddError(errors.Errorf("programmer error: length of statuses != length of hosts"))
				continue clientsLoop
			}
			hostIDs := []string{}
			for _, h := range hosts {
				hostIDs = append(hostIDs, h.Id)
			}
			for i := range hosts {
				j.AddError(errors.Wrap(j.setCloudHostStatus(ctx, m, hosts[i], statuses[i]), "error settings cloud host status"))
			}
			continue clientsLoop
		}
		for _, h := range hosts {
			hostStatus, err := m.GetInstanceStatus(ctx, &h)
			if err != nil {
				j.AddError(errors.Wrapf(err, "error checking instance status of host %s", h.Id))
				continue clientsLoop
			}
			j.AddError(errors.Wrap(j.setCloudHostStatus(ctx, m, h, hostStatus), "error settings instance statuses"))
		}
	}
}

func (j *cloudHostReadyJob) terminateUnknownHosts(ctx context.Context, awsErr string) error {
	pieces := strings.Split(awsErr, "'")
	if len(pieces) != 3 {
		return errors.Errorf("unexpected format of AWS error: %s", awsErr)
	}
	instanceIDs := strings.Split(pieces[1], ",")
	grip.Warning(message.Fields{
		"message": "host IDs not found in AWS, will terminate",
		"hosts":   instanceIDs,
	})
	catcher := grip.NewBasicCatcher()
	for _, hostID := range instanceIDs {
		h, err := host.FindOneId(hostID)
		if err != nil {
			catcher.Add(err)
			continue
		}
		if h == nil {
			continue
		}
		catcher.Add(j.env.RemoteQueue().Put(ctx, NewHostTerminationJob(j.env, h, true, "instance ID not found")))
	}
	return catcher.Resolve()
}

// setCloudHostStatus sets the host's status to HostProvisioning if host is running.
func (j *cloudHostReadyJob) setCloudHostStatus(ctx context.Context, m cloud.Manager, h host.Host, hostStatus cloud.CloudStatus) error {
	switch hostStatus {
	case cloud.StatusFailed, cloud.StatusTerminated:
		grip.Debug(message.Fields{
			"ticket":     "EVG-6100",
			"message":    "host status",
			"host_id":    h,
			"hostStatus": hostStatus.String(),
		})
		return errors.Wrap(m.TerminateInstance(ctx, &h, evergreen.User, "cloud provider reported host failed to start"), "error terminating instance")
	case cloud.StatusRunning:
		if err := j.initialSetup(ctx, m, &h); err != nil {
			return errors.Wrap(err, "problem doing initial setup")
		}
		return j.setNextState(h)
	}

	grip.Info(message.Fields{
		"message": "host not ready for setup",
		"host_id": h.Id,
		"DNS":     h.Host,
		"distro":  h.Distro.Id,
		"runner":  "hostinit",
		"status":  hostStatus,
	})
	return nil
}

func (j *cloudHostReadyJob) setNextState(h host.Host) error {
	switch h.Distro.BootstrapSettings.Method {
	case distro.BootstrapMethodUserData:
		// We're done provisioning user data hosts. The user data script will do the rest.
		return errors.Wrapf(h.SetProvisionedNotRunning(), "error marking host %s as provisioned not running", h.Id)
	case distro.BootstrapMethodNone:
		// hosts created by tasks are not provisioned so we can skip the provisioning state.
		return errors.Wrapf(h.MarkAsProvisioned(), "error marking host %s as running", h.Id)
	default:
		return errors.Wrap(h.SetProvisioning(), "error setting host to provisioning")
	}
}

func (j *cloudHostReadyJob) initialSetup(ctx context.Context, cloudMgr cloud.Manager, h *host.Host) error {
	if err := cloudMgr.OnUp(ctx, h); err != nil {
		return errors.Wrapf(err, "OnUp callback failed for host %s", h.Id)
	}
	return j.setDNSName(ctx, cloudMgr, h)
}

func (j *cloudHostReadyJob) setDNSName(ctx context.Context, cloudMgr cloud.Manager, h *host.Host) error {
	if h.Host != "" {
		return nil
	}

	// get the DNS name for the host
	hostDNS, err := cloudMgr.GetDNSName(ctx, h)
	if err != nil {
		return errors.Wrapf(err, "error checking DNS name for host %s", h.Id)
	}

	// sanity check for the host DNS name
	if hostDNS == "" {
		// DNS name not required if IP address set
		if h.IP != "" {
			return nil
		}
		return errors.Errorf("instance %s is running but not returning a DNS name or IP address", h.Id)
	}

	// update the host's DNS name
	if err = h.SetDNSName(hostDNS); err != nil {
		return errors.Wrapf(err, "error setting DNS name for host %s", h.Id)
	}

	return nil
}
