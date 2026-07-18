// factories.go provides DB-backed builders for the base tables. Each factory
// inserts a valid row with random unique identifiers and returns the generated
// type, so tests only override what they care about. Factories are safe under
// t.Parallel() because every factory mints a fresh UUID-based slug/email.

//go:build integration

package testutil

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// querier builds a *gen.Queries on top of the given exec target.
func querier(db gen.DBTX) *gen.Queries { return gen.New(db) }

// NewTenant inserts and returns a fresh, non-deleted tenant.
func NewTenant(ctx context.Context, t *testing.T, db gen.DBTX) gen.Tenant {
	t.Helper()
	suffix := uuid.NewString()[:8]
	tn, err := querier(db).CreateTenant(ctx, gen.CreateTenantParams{
		Slug:     "tenant-" + suffix,
		Name:     "Tenant " + suffix,
		IsActive: true,
	})
	require.NoError(t, err, "create tenant")
	return tn
}

// NewUser inserts and returns a fresh, non-deleted user with a unique email and
// no password hash (OAuth-only). Pass withPassword=true to set a placeholder.
func NewUser(ctx context.Context, t *testing.T, db gen.DBTX, withPassword bool) gen.User {
	t.Helper()
	suffix := uuid.NewString()[:8]
	var pw *string
	if withPassword {
		s := "argon2id$fake$hash$" + suffix
		pw = &s
	}
	u, err := querier(db).CreateUser(ctx, gen.CreateUserParams{
		Email:        "user-" + suffix + "@example.test",
		PasswordHash: pw,
		IsActive:     true,
	})
	require.NoError(t, err, "create user")
	return u
}

// NewRole inserts and returns a fresh role with the given slug/name.
func NewRole(ctx context.Context, t *testing.T, db gen.DBTX, slug, name string) gen.Role {
	t.Helper()
	r, err := querier(db).CreateRole(ctx, gen.CreateRoleParams{
		Slug:     slug,
		Name:     name,
		IsSystem: false,
	})
	require.NoError(t, err, "create role")
	return r
}

// NewMembership inserts and returns a membership linking userID into tenantID
// with an optional roleID. Unlike the tenant-scoped repository, the factory
// takes tenantID explicitly because tests set up their own tenant scope.
func NewMembership(
	ctx context.Context,
	t *testing.T,
	db gen.DBTX,
	tenantID, userID uuid.UUID,
	roleID *uuid.UUID,
) gen.Membership {
	t.Helper()
	m, err := querier(db).CreateMembership(ctx, gen.CreateMembershipParams{
		TenantID: tenantID,
		UserID:   userID,
		RoleID:   roleID,
	})
	require.NoError(t, err, "create membership")
	return m
}

// NewAuditLog inserts and returns an audit row. It bypasses the trigger-safe
// path only in the sense that it inserts directly; the append-only trigger does
// not block INSERT.
func NewAuditLog(
	ctx context.Context,
	t *testing.T,
	db gen.DBTX,
	tenantID *uuid.UUID,
	action string,
) gen.AuditLog {
	t.Helper()
	a, err := querier(db).CreateAuditLog(ctx, gen.CreateAuditLogParams{
		TenantID:     tenantID,
		ActorType:    "user",
		Action:       action,
		ResourceType: "test_resource",
		Status:       "success",
		Metadata:     json.RawMessage(`{}`),
	})
	require.NoError(t, err, "create audit log")
	return a
}

// NewOAuthIdentity inserts and returns a fresh external-identity link for the
// given user. Provider is "google" by default; pass a different value to test
// other IdPs. The tokens are stored as opaque ciphertext in production (the
// app layer encrypts); for tests we store a fake string.
func NewOAuthIdentity(
	ctx context.Context,
	t *testing.T,
	db gen.DBTX,
	userID uuid.UUID,
	provider string,
) gen.UserOauthIdentity {
	t.Helper()
	if provider == "" {
		provider = "google"
	}
	subject := "sub-" + uuid.NewString()[:12]
	token := "enc::fake-token::" + uuid.NewString()[:8]
	row, err := querier(db).CreateOAuthIdentity(ctx, gen.CreateOAuthIdentityParams{
		UserID:       userID,
		Provider:     provider,
		Subject:      subject,
		AccessToken:  &token,
		RefreshToken: &token,
		Scopes:       []string{"openid", "email"},
	})
	require.NoError(t, err, "create oauth identity")
	return row
}
