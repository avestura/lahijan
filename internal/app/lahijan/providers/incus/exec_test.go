// Package incus: exec_test.go covers the exec websocket flow. The test
// round-trips a command via the fake daemon: the driver POSTs /exec, opens
// the per-fd websockets, captures stdout, and reads the exit code.
package incus_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExec_RoundTripsCommand(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	// Custom handler: echo the command joined by spaces + a known suffix.
	srv.SetExecHandler(func(_, _ string, params incus.InstanceExecPost) ([]byte, []byte, int) {
		out := []byte(strings.Join(params.Command, " ") + ":ok")
		return out, nil, 0
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	// Create a target instance for the exec to run against.
	_, err := p.CreateInstance(ctx, incus.CreateInstanceParams{
		Project: project,
		Name:    "exec-target",
		Source:  incus.InstanceSource{Type: "image", Alias: "ubuntu/24.04"},
	})
	require.NoError(t, err)

	res, err := p.Exec(ctx, incus.ExecParams{
		Project:  project,
		Instance: "exec-target",
		Command:  []string{"echo", "hello"},
		Stdin:    nil,
		Timeout:  5 * time.Second,
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, 0, res.ExitCode, "exit code must be 0 on success")
	assert.Equal(t, "echo hello:ok", string(res.Stdout),
		"stdout must round-trip the handler's output")
}

func TestExec_ExitCode_Propagates(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	srv.SetExecHandler(func(_, _ string, _ incus.InstanceExecPost) ([]byte, []byte, int) {
		return []byte("stderr-from-handler"), []byte("real-stderr"), 42
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	_, err := p.CreateInstance(ctx, incus.CreateInstanceParams{
		Project: project,
		Name:    "exit-target",
		Source:  incus.InstanceSource{Type: "image", Alias: "ubuntu/24.04"},
	})
	require.NoError(t, err)

	res, err := p.Exec(ctx, incus.ExecParams{
		Project:  project,
		Instance: "exit-target",
		Command:  []string{"false"},
		Timeout:  5 * time.Second,
	})
	require.NoError(t, err)
	assert.Equal(t, 42, res.ExitCode, "non-zero exit code must propagate")
	assert.Equal(t, "stderr-from-handler", string(res.Stdout))
}

func TestExec_MissingInstance_ReturnsError(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	_, err := p.Exec(ctx, incus.ExecParams{
		Project:  project,
		Instance: "does-not-exist",
		Command:  []string{"echo", "hi"},
		Timeout:  2 * time.Second,
	})
	require.Error(t, err, "exec against a missing instance must error")
}

func TestExec_EmptyCommand_ReturnsError(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := p.Exec(ctx, incus.ExecParams{
		Project:  "default",
		Instance: "whatever",
		Command:  nil,
	})
	require.Error(t, err, "empty command must error before round-trip")
}
