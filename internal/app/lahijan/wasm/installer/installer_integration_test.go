// installer_integration_test.go exercises the installer.Service end-to-end
// against a real Postgres. The runtime is wired with a real wazero.Runtime so
// Upload actually compiles the wasm bytes.

//go:build integration

package installer_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/installer"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/manifest"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/runtime"
)

// TestMain starts one testcontainer Postgres for this package.
func TestMain(m *testing.M) { testutil.Setup(m) }

// discardWriter swallows slog output so the test logs are quiet.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// newService builds a fresh installer.Service with a real runtime + DB repo.
// The audit emitter is real (DB-backed) so tests can assert on the rows.
func newService(t *testing.T) (*installer.Service, *runtime.Runtime) {
	t.Helper()
	rt, err := runtime.New(context.Background(), permission.NewMapEnforcer(), runtime.Config{
		MaxMemoryBytes: 8 * runtime.WasmPageBytes,
		ExecTimeout:    500 * time.Millisecond,
		Logger:         slog.New(slog.NewTextHandler(&discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError})),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	repos := testutil.Repos()
	svc := installer.New(repos.Plugins, rt, audit.NewDBEmitter(repos.AuditLog))
	return svc, rt
}

// addModule returns a tiny valid wasm module (same bytes as runtime.addModule).
// Duplicated here to avoid an internal-test-only import.
func addModule() []byte {
	return []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x07, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f,
		0x03, 0x02, 0x01, 0x00,
		0x05, 0x04, 0x01, 0x01, 0x01, 0x01,
		0x07, 0x10, 0x02,
		0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00,
		0x03, 0x61, 0x64, 0x64, 0x00, 0x00,
		0x0a, 0x09, 0x01, 0x07, 0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b,
	}
}

func TestUpload_HappyPath_PersistsAndAudits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	actor := testutil.NewUser(ctx, t, pool, true)

	m := &manifest.Manifest{Name: "happy-plugin", Version: "1.0.0"}
	row, err := svc.Upload(ctx, installer.UploadParams{
		TenantID:    &tenant.ID,
		ActorUserID: actor.ID,
		WasmBytes:   addModule(),
		Manifest:    m,
	})
	require.NoError(t, err)
	assert.Equal(t, "happy-plugin", row.Name)
	assert.Equal(t, "pending", row.Status)
	assert.NotEmpty(t, row.WasmHash)

	// Audit row was emitted.
	var n int
	err = pool.QueryRow(
		ctx,
		"SELECT count(*) FROM audit_log WHERE action = $1 AND resource_id = $2",
		installer.ActionUpload, row.ID,
	).Scan(&n)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, n, 1, "expected an audit row for the upload")
}

func TestUpload_DuplicateNameVersionFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	actor := testutil.NewUser(ctx, t, pool, true)

	m := &manifest.Manifest{Name: "dup", Version: "1.0.0"}
	_, err := svc.Upload(ctx, installer.UploadParams{
		TenantID: &tenant.ID, ActorUserID: actor.ID,
		WasmBytes: addModule(), Manifest: m,
	})
	require.NoError(t, err)

	_, err = svc.Upload(ctx, installer.UploadParams{
		TenantID: &tenant.ID, ActorUserID: actor.ID,
		WasmBytes: addModule(), Manifest: m,
	})
	require.ErrorIs(t, err, installer.ErrDuplicateUpload)
}

func TestUpload_RejectsInvalidManifest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	actor := testutil.NewUser(ctx, t, pool, true)

	// Name has uppercase -> validate fails.
	m := &manifest.Manifest{Name: "BAD", Version: "1.0.0"}
	_, err := svc.Upload(ctx, installer.UploadParams{
		TenantID: &tenant.ID, ActorUserID: actor.ID,
		WasmBytes: addModule(), Manifest: m,
	})
	require.ErrorIs(t, err, installer.ErrManifestInvalid)
}

func TestGrant_RejectsUnknownPermission(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	actor := testutil.NewUser(ctx, t, pool, true)

	row := testutil.NewPlugin(ctx, t, pool, &tenant.ID, "")
	err := svc.Grant(ctx, row.ID, actor.ID, "totally.bogus", &tenant.ID, nil)
	require.Error(t, err)
}

func TestGrant_HappyPath_PersistsAndAudits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	actor := testutil.NewUser(ctx, t, pool, true)

	row := testutil.NewPlugin(ctx, t, pool, &tenant.ID, "")
	err := svc.Grant(ctx, row.ID, actor.ID, "kv.read:cache", &tenant.ID, nil)
	require.NoError(t, err)

	grants, err := testutil.Repos().Plugins.ListPermissions(ctx, row.ID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	assert.Equal(t, "kv.read:cache", grants[0].Permission)

	var n int
	err = pool.QueryRow(
		ctx,
		"SELECT count(*) FROM audit_log WHERE action = $1 AND resource_id = $2",
		installer.ActionGrant, row.ID,
	).Scan(&n)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, n, 1)
}

func TestRevoke_RemovesGrantAndAudits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	actor := testutil.NewUser(ctx, t, pool, true)

	row := testutil.NewPlugin(ctx, t, pool, &tenant.ID, "")
	require.NoError(t, svc.Grant(ctx, row.ID, actor.ID, "events.emit", &tenant.ID, nil))
	require.NoError(t, svc.Revoke(ctx, row.ID, actor.ID, "events.emit", &tenant.ID, nil))

	grants, err := testutil.Repos().Plugins.ListPermissions(ctx, row.ID)
	require.NoError(t, err)
	assert.Empty(t, grants)
}

func TestEnableDisable_LifecycleAndAudit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	actor := testutil.NewUser(ctx, t, pool, true)

	row := testutil.NewPlugin(ctx, t, pool, &tenant.ID, "")

	require.NoError(t, svc.Enable(ctx, row.ID, actor.ID, &tenant.ID, nil))
	got, err := testutil.Repos().Plugins.Get(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, database.PluginStatusActive, got.Status)

	require.NoError(t, svc.Disable(ctx, row.ID, actor.ID, &tenant.ID, nil))
	got, err = testutil.Repos().Plugins.Get(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, database.PluginStatusDisabled, got.Status)

	for _, action := range []string{installer.ActionEnable, installer.ActionDisable} {
		var n int
		err = pool.QueryRow(
			ctx,
			"SELECT count(*) FROM audit_log WHERE action = $1 AND resource_id = $2",
			action, row.ID,
		).Scan(&n)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, n, 1)
	}
}

func TestDelete_RemovesRowAndAudits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	actor := testutil.NewUser(ctx, t, pool, true)

	row := testutil.NewPlugin(ctx, t, pool, &tenant.ID, "")
	require.NoError(t, svc.Delete(ctx, row.ID, actor.ID, &tenant.ID, nil))

	_, err := testutil.Repos().Plugins.Get(ctx, row.ID)
	require.Error(t, err)

	var n int
	err = pool.QueryRow(
		ctx,
		"SELECT count(*) FROM audit_log WHERE action = $1 AND resource_id = $2",
		installer.ActionDelete, row.ID,
	).Scan(&n)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, n, 1)
}

func TestActions_OnMissingPlugin_ReturnErrNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	bogus := uuid.New()
	actor := uuid.New()

	require.ErrorIs(t, svc.Grant(ctx, bogus, actor, "kv.read:cache", nil, nil), installer.ErrNotFound)
	require.ErrorIs(t, svc.Revoke(ctx, bogus, actor, "kv.read:cache", nil, nil), installer.ErrNotFound)
	require.ErrorIs(t, svc.Enable(ctx, bogus, actor, nil, nil), installer.ErrNotFound)
	require.ErrorIs(t, svc.Disable(ctx, bogus, actor, nil, nil), installer.ErrNotFound)
	require.ErrorIs(t, svc.Delete(ctx, bogus, actor, nil, nil), installer.ErrNotFound)
}
