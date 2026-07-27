// Package incus: projects_test.go covers the project mapping and the
// restricted-defaults bootstrap. These are the most important security-
// critical tests in WS-11: they prove a tenant's project is created with the
// restricted-* config + features.* flags and that the tenant id can be
// recovered from the project name.
package incus_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectName_Format(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	tenantID := uuid.New()
	assert.Equal(t, "lahijan-tenant-"+tenantID.String(), p.ProjectName(tenantID),
		"ProjectName must follow the ADR-0010 format")
}

func TestProjectName_CustomPrefix(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	cli := newHTTPClient()
	p, err := incus.NewClient(incus.Config{
		HTTPClient:    cli,
		BaseURL:       srv.HTTP.URL,
		ProjectPrefix: "tenant-",
	})
	require.NoError(t, err)
	tenantID := uuid.New()
	assert.Equal(t, "tenant-"+tenantID.String(), p.ProjectName(tenantID))
}

func TestTenantIDFromProject_RoundTrips(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	tenantID := uuid.New()
	project := p.ProjectName(tenantID)
	got, err := p.TenantIDFromProject(project)
	require.NoError(t, err)
	assert.Equal(t, tenantID, got, "TenantIDFromProject must reverse ProjectName")
}

func TestTenantIDFromProject_RejectsForeignPrefix(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	_, err := p.TenantIDFromProject("someone-else-tenant-" + uuid.NewString())
	require.Error(t, err, "Foreign-prefixed project must error")
}

func TestTenantIDFromProject_RejectsBadUUID(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)
	_, err := p.TenantIDFromProject("lahijan-tenant-not-a-uuid")
	require.Error(t, err)
}

func TestRestrictedProjectDefaults_ContainsAllGuards(t *testing.T) {
	t.Parallel()
	cfg := incus.RestrictedProjectDefaults()
	// Spot-check the most important guards — they are the ones the WS-11
	// DoD calls out (a tenant can't see another tenant's resources through
	// Incus). Key names are Incus 6.0 LTS schema (verified against
	// `incus project set` on 6.0.0).
	assert.Equal(t, "true", cfg["restricted"], "restricted master flag must be on")
	assert.Equal(t, "block", cfg["restricted.devices.gpu"], "GPU passthrough must be blocked")
	assert.Equal(t, "block", cfg["restricted.devices.usb"], "USB passthrough must be blocked")
	assert.Equal(t, "block", cfg["restricted.devices.unix-block"], "Unix-block devices must be blocked")
	assert.Equal(t, "block", cfg["restricted.devices.unix-char"], "Unix-char devices must be blocked")
	assert.Equal(t, "managed", cfg["restricted.devices.nic"], "NIC must be restricted to managed networks")
	assert.Equal(t, "block", cfg["restricted.networks.uplinks"], "Uplinks must be blocked")
	assert.Equal(t, "block", cfg["restricted.containers.lowlevel"], "Low-level container config must be blocked")
	assert.Equal(t, "block", cfg["restricted.virtual-machines.lowlevel"], "Low-level VM config must be blocked")
	// Sanity: the pre-6.0 keys must NOT be present (Incus 6.0 rejects them).
	_, hasOldRestrict := cfg["restrict"]
	assert.False(t, hasOldRestrict, "pre-6.0 'restrict' key must be removed (Incus 6.0 rejects it)")
	_, hasOldUnix := cfg["restricted.devices.unix"]
	assert.False(t, hasOldUnix, "pre-6.0 'restricted.devices.unix' key must be removed (Incus 6.0 splits it into unix-block + unix-char)")
}

func TestEnsureProject_CreatesProjectWithRestrictedDefaults(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID), "EnsureProject must create the project")

	// Fetch back via the low-level GetProject to assert the config landed.
	prj, err := p.GetProject(ctx, p.ProjectName(tenantID))
	require.NoError(t, err)
	assert.Equal(t, "true", prj.Config["restricted"], "project must have restricted=true")
	assert.Equal(t, "block", prj.Config["restricted.devices.gpu"])
	assert.Equal(t, "true", prj.Config["features.images"], "feature flag must be on by default")
	assert.Equal(t, "true", prj.Config["features.profiles"])
	assert.Equal(t, "true", prj.Config["features.networks"])
}

func TestEnsureProject_Idempotent(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	// Second call must NOT fail with ErrAlreadyExists; it should silently
	// update the config in place.
	require.NoError(t, p.EnsureProject(ctx, tenantID))

	projects, err := p.ListProjects(ctx)
	require.NoError(t, err)
	count := 0
	for _, pr := range projects {
		if pr.Name == p.ProjectName(tenantID) {
			count++
		}
	}
	assert.Equal(t, 1, count, "EnsureProject must not duplicate the project")
}

func TestEnsureProject_SeedsDefaultProfile(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	// The auto-seeded default profile must exist and carry a root disk on
	// the daemon's first storage pool (the fake pre-seeds "default").
	prf, err := p.GetProfile(ctx, project, "default")
	require.NoError(t, err, "EnsureProject must seed a default profile")
	require.Contains(t, prf.Devices, "root", "default profile must have a root device")
	assert.Equal(t, "disk", prf.Devices["root"]["type"])
	assert.Equal(t, "/", prf.Devices["root"]["path"])
	assert.Equal(t, "default", prf.Devices["root"]["pool"], "root disk must use the daemon's first pool")
	// eth0 NIC is best-effort: seedDefaultProfile adds it only when the
	// daemon exposes a managed bridge. The fake models networks as
	// project-scoped (real Incus treats managed bridges as cluster-wide),
	// so a freshly-created project sees no bridge and eth0 is skipped.
	// Real daemons (incusbr0 / lahijanbr) DO expose the bridge to every
	// project, so the eth0 is present in production.
}

func TestCreateProject_AlreadyExists(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	name := "dup-project-" + uuid.NewString()
	require.NoError(t, p.CreateProject(ctx, name, "first", nil))
	err := p.CreateProject(ctx, name, "second", nil)
	require.Error(t, err, "second create must fail")
	assert.True(t, errors.Is(err, incus.ErrAlreadyExists),
		"duplicate create must return ErrAlreadyExists, got %v", err)
}

func TestDeleteProject(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	name := "to-delete-" + uuid.NewString()
	require.NoError(t, p.CreateProject(ctx, name, "", nil))
	require.NoError(t, p.DeleteProject(ctx, name))

	_, err := p.GetProject(ctx, name)
	require.Error(t, err, "deleted project must not be retrievable")
}
