package units

import (
	"context"
	"testing"

	"github.com/evergreen-ci/birch"

	"github.com/evergreen-ci/evergreen"
	"github.com/evergreen-ci/evergreen/cloud"
	"github.com/evergreen-ci/evergreen/db"
	"github.com/evergreen-ci/evergreen/mock"
	"github.com/evergreen-ci/evergreen/model/distro"
	"github.com/evergreen-ci/evergreen/model/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
)

func TestCloudStatusJob(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	require.NoError(db.ClearCollections(host.Collection))
	hosts := []host.Host{
		{
			Id:       "host-1",
			Provider: evergreen.ProviderNameMock,
			Status:   evergreen.HostStarting,
			Distro: distro.Distro{
				Provider:             evergreen.ProviderNameMock,
				ProviderSettingsList: []*birch.Document{birch.NewDocument(birch.EC.String("region", "region-1"))},
			},
		},
		{
			Id:       "host-2",
			Provider: evergreen.ProviderNameMock,
			Status:   evergreen.HostStarting,
			Distro: distro.Distro{
				Provider:             evergreen.ProviderNameMock,
				ProviderSettingsList: []*birch.Document{birch.NewDocument(birch.EC.String("region", "region-2"))},
			},
		},
		{
			Id:       "host-3",
			Provider: evergreen.ProviderNameMock,
			Status:   evergreen.HostTerminated,
		},
		{
			Id:       "host-4",
			Provider: evergreen.ProviderNameMock,
			Status:   evergreen.HostProvisioning,
		},
		{
			Id:       "host-5",
			Provider: evergreen.ProviderNameMock,
			Status:   evergreen.HostStarting,
			Distro: distro.Distro{
				Provider:             evergreen.ProviderNameMock,
				ProviderSettingsList: []*birch.Document{birch.NewDocument(birch.EC.String("region", "region-3"))},
				BootstrapSettings:    distro.BootstrapSettings{Method: distro.BootstrapMethodNone},
			},
		},
		{
			Id:       "host-6",
			Provider: evergreen.ProviderNameMock,
			Status:   evergreen.HostStarting,
			Distro: distro.Distro{
				Provider:             evergreen.ProviderNameMock,
				ProviderSettingsList: []*birch.Document{birch.NewDocument(birch.EC.String("region", "region-4"))},
				BootstrapSettings:    distro.BootstrapSettings{Method: distro.BootstrapMethodUserData},
			},
		},
	}
	mockState := cloud.GetMockProvider()
	mockState.Reset()
	for _, h := range hosts {
		require.NoError(h.Insert())
		mockState.Set(h.Id, cloud.MockInstance{DNSName: "dns_name"})
	}

	j := NewCloudHostReadyJob(&mock.Environment{}, "id")
	j.Run(context.Background())
	assert.NoError(j.Error())

	hosts, err := host.Find(db.Query(bson.M{}))
	assert.Len(hosts, 6)
	assert.NoError(err)
	for _, h := range hosts {
		if h.Id == "host-1" {
			assert.Equal(evergreen.HostProvisioning, h.Status)
		}
		if h.Id == "host-2" {
			assert.Equal(evergreen.HostProvisioning, h.Status)
		}
		if h.Id == "host-3" {
			assert.Equal(evergreen.HostTerminated, h.Status)
		}
		if h.Id == "host-4" {
			assert.Equal(evergreen.HostProvisioning, h.Status)
		}
		if h.Id == "host-5" {
			assert.Equal(evergreen.HostRunning, h.Status)
		}
		if h.Id == "host-6" {
			assert.Equal(evergreen.HostStarting, h.Status)
			assert.True(h.Provisioned)
		}
	}
}

func TestTerminateUnknownHosts(t *testing.T) {
	require.NoError(t, db.ClearCollections(host.Collection))
	h1 := host.Host{
		Id: "h1",
	}
	require.NoError(t, h1.Insert())
	h2 := host.Host{
		Id: "h2",
	}
	require.NoError(t, h2.Insert())
	env := &mock.Environment{}
	ctx := context.Background()
	require.NoError(t, env.Configure(ctx))
	j := NewCloudHostReadyJob(env, "id").(*cloudHostReadyJob)
	awsErr := "error getting host statuses for providers: error describing instances: after 10 retries, operation failed: InvalidInstanceID.NotFound: The instance IDs 'h1, h2' do not exist"
	assert.NoError(t, j.terminateUnknownHosts(ctx, awsErr))
}
