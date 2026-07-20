package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// fakeDeps is a hand-rolled in-memory Deps implementation for the bootstrap
// unit tests. The bootstrap touches a tiny surface (4 repos + a hasher) so
// a full testcontainers harness would be overkill; these fakes exercise
// every code path the test cases below enumerate.
type fakeDeps struct {
	users       *fakeUsers
	tenants     TenantStore
	roles       *fakeRoles
	memberships *fakeMemberships
	hasher      *fakeHasher
}

func (d *fakeDeps) Users() UserStore             { return d.users }
func (d *fakeDeps) Tenants() TenantStore         { return d.tenants }
func (d *fakeDeps) Roles() RoleStore             { return d.roles }
func (d *fakeDeps) Memberships() MembershipStore { return d.memberships }
func (d *fakeDeps) Hasher() Hasher               { return d.hasher }

type fakeUsers struct {
	count int64
	rows  []database.User
}

func (u *fakeUsers) Count(context.Context) (int64, error) { return u.count, nil }
func (u *fakeUsers) Create(_ context.Context, arg database.CreateUserParams) (database.User, error) {
	usr := database.User{
		ID:           uuid.New(),
		Email:        arg.Email,
		PasswordHash: arg.PasswordHash,
		IsActive:     true,
		DisplayName:  arg.DisplayName,
	}
	u.rows = append(u.rows, usr)
	u.count++
	return usr, nil
}

type fakeTenants struct{ rows []database.Tenant }

func (t *fakeTenants) Create(_ context.Context, arg database.CreateTenantParams) (database.Tenant, error) {
	tenant := database.Tenant{ID: uuid.New(), Slug: arg.Slug, Name: arg.Name, IsActive: true}
	t.rows = append(t.rows, tenant)
	return tenant, nil
}

type fakeRoles struct{ bySlug map[string]database.Role }

func (r *fakeRoles) GetRoleBySlug(_ context.Context, slug string) (database.Role, error) {
	role, ok := r.bySlug[slug]
	if !ok {
		return database.Role{}, errors.New("role not found")
	}
	return role, nil
}

type fakeMemberships struct{ rows []database.Membership }

func (m *fakeMemberships) Create(ctx context.Context, userID uuid.UUID, roleID *uuid.UUID) (database.Membership, error) {
	tenantID, err := database.TenantFromContext(ctx)
	if err != nil {
		return database.Membership{}, err
	}
	row := database.Membership{ID: uuid.New(), TenantID: tenantID, UserID: userID, RoleID: roleID}
	m.rows = append(m.rows, row)
	return row, nil
}

type fakeHasher struct{ lastHashed string }

func (h *fakeHasher) Hash(pw string) (string, error) {
	h.lastHashed = pw
	return "argon2id$fake$" + pw, nil
}

func newFakeDeps() *fakeDeps {
	adminRoleID := uuid.New()
	return &fakeDeps{
		users:       &fakeUsers{count: 0},
		tenants:     &fakeTenants{},
		roles:       &fakeRoles{bySlug: map[string]database.Role{rbac.RolePlatformAdmin: {ID: adminRoleID, Slug: rbac.RolePlatformAdmin}}},
		memberships: &fakeMemberships{},
		hasher:      &fakeHasher{},
	}
}

func TestEnsurePlatformAdmin_Disabled(t *testing.T) {
	t.Parallel()
	deps := newFakeDeps()
	_, err := EnsurePlatformAdmin(t.Context(), deps, Config{Enabled: false, AdminEmail: "a@b.c"}, slog.Default())
	require.ErrorIs(t, err, ErrDisabled)
	assert.Equal(t, int64(0), deps.users.count, "no user should be created when disabled")
}

func TestEnsurePlatformAdmin_NoEmail(t *testing.T) {
	t.Parallel()
	deps := newFakeDeps()
	_, err := EnsurePlatformAdmin(t.Context(), deps, Config{Enabled: true, AdminEmail: ""}, slog.Default())
	require.ErrorIs(t, err, ErrNoEmail)
}

func TestEnsurePlatformAdmin_AlreadyPopulated(t *testing.T) {
	t.Parallel()
	deps := newFakeDeps()
	deps.users.count = 1
	_, err := EnsurePlatformAdmin(t.Context(), deps, Config{Enabled: true, AdminEmail: "a@b.c"}, slog.Default())
	require.ErrorIs(t, err, ErrAlreadyPopulated)
}

func TestEnsurePlatformAdmin_PresetPassword(t *testing.T) {
	t.Parallel()
	deps := newFakeDeps()
	cfg := Config{
		Enabled:          true,
		AdminEmail:       "admin@example.com",
		AdminPassword:    "Tr0ub4dour&3",
		AdminDisplayName: "Operator",
	}
	res, err := EnsurePlatformAdmin(t.Context(), deps, cfg, slog.Default())
	require.NoError(t, err)
	require.True(t, res.Created)
	assert.Equal(t, "Tr0ub4dour&3", res.RawPassword)
	require.Len(t, deps.users.rows, 1)
	assert.Equal(t, "admin@example.com", deps.users.rows[0].Email)
	require.NotNil(t, deps.users.rows[0].PasswordHash)
	assert.Equal(t, "argon2id$fake$Tr0ub4dour&3", *deps.users.rows[0].PasswordHash)
	require.Len(t, deps.tenants.(*fakeTenants).rows, 1)
	assert.Equal(t, "default", deps.tenants.(*fakeTenants).rows[0].Slug)
	require.Len(t, deps.memberships.rows, 1)
	assert.Equal(t, deps.users.rows[0].ID, deps.memberships.rows[0].UserID)
	assert.Equal(t, deps.tenants.(*fakeTenants).rows[0].ID, deps.memberships.rows[0].TenantID)
	require.NotNil(t, deps.memberships.rows[0].RoleID)
	assert.Equal(t, deps.roles.bySlug[rbac.RolePlatformAdmin].ID, *deps.memberships.rows[0].RoleID)
}

func TestEnsurePlatformAdmin_GeneratesPassword(t *testing.T) {
	t.Parallel()
	deps := newFakeDeps()
	cfg := Config{
		Enabled:                 true,
		AdminEmail:              "admin@example.com",
		GeneratedPasswordLength: 32,
	}
	res, err := EnsurePlatformAdmin(t.Context(), deps, cfg, slog.Default())
	require.NoError(t, err)
	require.True(t, res.Created)
	// 32 chars over the 64-char alphabet; verify length + charset.
	assert.Len(t, res.RawPassword, 32)
	for _, r := range res.RawPassword {
		assert.True(t, strings.ContainsRune(passwordAlphabet, r), "unexpected char %q in generated password", r)
	}
	require.NotNil(t, deps.users.rows[0].PasswordHash)
	assert.Equal(t, "argon2id$fake$"+res.RawPassword, *deps.users.rows[0].PasswordHash,
		"stored hash should match the generated raw password")
}

func TestEnsurePlatformAdmin_RoleLookupFails(t *testing.T) {
	t.Parallel()
	deps := newFakeDeps()
	deps.roles.bySlug = map[string]database.Role{} // simulate missing role
	_, err := EnsurePlatformAdmin(t.Context(), deps, Config{Enabled: true, AdminEmail: "a@b.c"}, slog.Default())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve platform.admin role")
}

type brokenTenants struct{ err error }

func (t *brokenTenants) Create(context.Context, database.CreateTenantParams) (database.Tenant, error) {
	return database.Tenant{}, t.err
}

func TestEnsurePlatformAdmin_TenantCreateFails(t *testing.T) {
	t.Parallel()
	deps := newFakeDeps()
	deps.tenants = &brokenTenants{err: errors.New("db unavailable")}
	_, err := EnsurePlatformAdmin(t.Context(), deps, Config{Enabled: true, AdminEmail: "a@b.c"}, slog.Default())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create default tenant")
}

func TestGeneratePassword_CharsetAndLength(t *testing.T) {
	t.Parallel()
	cases := []int{1, 12, 24, 64}
	for _, n := range cases {
		pw, err := generatePassword(n)
		require.NoError(t, err)
		assert.Len(t, pw, n)
		for _, r := range pw {
			assert.True(t, strings.ContainsRune(passwordAlphabet, r))
		}
	}
}

func TestGeneratePassword_NonPositiveLength(t *testing.T) {
	t.Parallel()
	_, err := generatePassword(0)
	require.Error(t, err)
	_, err = generatePassword(-5)
	require.Error(t, err)
}

// TestResult_ZeroValueOnSkip ensures the Result struct is zero-valued on
// every skip path so the caller cannot accidentally log stale credentials
// from a prior run.
func TestResult_ZeroValueOnSkip(t *testing.T) {
	t.Parallel()
	deps := newFakeDeps()
	res, _ := EnsurePlatformAdmin(t.Context(), deps, Config{Enabled: false, AdminEmail: "a@b.c"}, slog.Default())
	assert.False(t, res.Created)
	assert.Empty(t, res.RawPassword)
	assert.Equal(t, uuid.Nil, res.TenantID)
	assert.Equal(t, uuid.Nil, res.UserID)
}
