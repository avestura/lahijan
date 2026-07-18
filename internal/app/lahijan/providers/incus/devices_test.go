// Package incus: devices_test.go covers the device attach/detach helpers.
package incus_test

import (
	"context"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDevice_AttachAndDetach(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	_, err := p.CreateInstance(ctx, incus.CreateInstanceParams{
		Project: project,
		Name:    "dev-target",
		Source:  incus.InstanceSource{Type: "image", Alias: "ubuntu/24.04"},
	})
	require.NoError(t, err)

	// Attach a NIC.
	require.NoError(t, p.AttachDevice(ctx, project, "dev-target", "nic-eth0", "nic",
		map[string]string{"nictype": "bridged", "parent": "lahijanbr", "ipv4.address": "10.0.0.5"}))

	got, err := p.GetInstance(ctx, project, "dev-target")
	require.NoError(t, err)
	require.Contains(t, got.Devices, "nic-eth0")
	assert.Equal(t, "nic", got.Devices["nic-eth0"]["type"])
	assert.Equal(t, "10.0.0.5", got.Devices["nic-eth0"]["ipv4.address"])

	// Attach a proxy.
	require.NoError(t, p.AttachDevice(ctx, project, "dev-target", "proxy-ssh", "proxy",
		map[string]string{"listen": "tcp:0.0.0.0:2222", "connect": "tcp:127.0.0.1:22"}))
	got, err = p.GetInstance(ctx, project, "dev-target")
	require.NoError(t, err)
	require.Contains(t, got.Devices, "proxy-ssh")

	// Detach the NIC.
	require.NoError(t, p.DetachDevice(ctx, project, "dev-target", "nic-eth0"))
	got, err = p.GetInstance(ctx, project, "dev-target")
	require.NoError(t, err)
	assert.NotContains(t, got.Devices, "nic-eth0", "detached device must be removed")
}

func TestDevice_DetachMissing_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	_, err := p.CreateInstance(ctx, incus.CreateInstanceParams{
		Project: project,
		Name:    "no-dev",
		Source:  incus.InstanceSource{Type: "image", Alias: "ubuntu/24.04"},
	})
	require.NoError(t, err)

	err = p.DetachDevice(ctx, project, "no-dev", "ghost")
	require.Error(t, err, "detaching a missing device must fail")
	assert.True(t, isErrorSentinel(err, incus.ErrNotFound),
		"detached-missing must return ErrNotFound")
}

// isErrorSentinel is a small wrapper so the test does not need to import
// errors just for one call.
func isErrorSentinel(err, target error) bool {
	if err == nil {
		return false
	}
	for e := err; e != nil; {
		if e == target {
			return true
		}
		type unwrapper interface{ Unwrap() error }
		if u, ok := e.(unwrapper); ok {
			e = u.Unwrap()
			continue
		}
		break
	}
	return false
}
