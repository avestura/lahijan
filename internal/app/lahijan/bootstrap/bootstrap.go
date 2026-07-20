// Package bootstrap implements Lahijan's first-run admin provisioning
// (WS-23). On a fresh database (zero non-deleted users) program.Start
// invokes EnsurePlatformAdmin, which creates a default tenant + a
// platform.admin user + a membership linking the two. The bootstrap is
// idempotent: it is a silent no-op as soon as ANY user exists, so a stable
// deploy never re-runs it.
//
// The credentials flow:
//   - When Config.AdminPassword is non-empty, it is hashed (argon2id) and
//     stored. The operator chose the password.
//   - When empty, EnsurePlatformAdmin generates a random one of
//     Config.GeneratedPasswordLength characters, hashes it, and returns
//     the raw value to the caller via Result.RawPassword so program.Start
//     can log it ONCE (the hash is the only thing persisted).
//
// The password generator uses crypto/rand over the url-safe base64 alphabet
// so the value is copy-pasteable without escaping. The default length of
// 24 yields ~143 bits of entropy.
package bootstrap

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// Sentinel errors. Callers translate these to startup-log messages.
var (
	// ErrDisabled is returned when Config.Enabled is false. Callers should
	// treat this as a silent skip (debug-level log).
	ErrDisabled = errors.New("bootstrap: disabled by config")

	// ErrNoEmail is returned when Config.Enabled is true but AdminEmail is
	// empty. Callers should treat this as a silent skip (debug-level log):
	// a deploy without LAHIJAN_BOOTSTRAP_ADMIN_EMAIL set simply does not
	// request the first-run admin.
	ErrNoEmail = errors.New("bootstrap: adminEmail not set; skipping")

	// ErrAlreadyPopulated is returned when the DB already has at least one
	// non-deleted user. Callers should treat this as a silent skip
	// (debug-level log): the bootstrap is one-shot.
	ErrAlreadyPopulated = errors.New("bootstrap: users already exist; skipping")
)

// Config carries the first-run bootstrap knobs read from conf. Build it
// once at startup from the conf.GetBootstrap* helpers.
type Config struct {
	// Enabled toggles the bootstrap. When false EnsurePlatformAdmin returns
	// ErrDisabled without touching the DB.
	Enabled bool
	// AdminEmail is the email of the platform.admin user to create. Empty
	// is treated as "skip bootstrap" even when Enabled is true.
	AdminEmail string
	// AdminPassword is the optional pre-set password. When empty the
	// bootstrap generates a random one of GeneratedPasswordLength chars.
	AdminPassword string
	// AdminDisplayName is written to the users.display_name column.
	AdminDisplayName string
	// GeneratedPasswordLength is the length of the random password minted
	// when AdminPassword is empty. Defaults to 24 when non-positive.
	GeneratedPasswordLength int
	// DefaultTenantSlug is the slug of the tenant created on first run.
	// Defaults to "default" when empty.
	DefaultTenantSlug string
	// DefaultTenantName is the display name of the default tenant.
	// Defaults to "Default Tenant" when empty.
	DefaultTenantName string
}

// Result is what EnsurePlatformAdmin returns on a successful first-run
// creation. The RawPassword field carries the un-hashed password exactly
// once so the caller can log it; it is NOT persisted anywhere by this
// package. Result is zero-valued (and RawPassword empty) for the no-op
// skip paths.
type Result struct {
	// Created is true when EnsurePlatformAdmin actually provisioned a new
	// admin user. False on every skip path (ErrDisabled / ErrNoEmail /
	// ErrAlreadyPopulated).
	Created bool
	// TenantID is the id of the default tenant created on first run.
	TenantID uuid.UUID
	// UserID is the id of the platform.admin user created on first run.
	UserID uuid.UUID
	// RawPassword is the un-hashed password. It equals Config.AdminPassword
	// when the operator pre-set it; otherwise it is the freshly-generated
	// random value. SENSITIVE — log once, never persist.
	RawPassword string
}

// Deps bundles the repositories + password hasher the bootstrap needs.
// *database.Repos + *password.Hasher satisfy this shape; tests pass mocks.
type Deps interface {
	Users() UserStore
	Tenants() TenantStore
	Roles() RoleStore
	Memberships() MembershipStore
	Hasher() Hasher
}

// UserStore is the subset of *database.UsersRepository the bootstrap calls.
type UserStore interface {
	Count(ctx context.Context) (int64, error)
	Create(ctx context.Context, arg database.CreateUserParams) (database.User, error)
}

// TenantStore is the subset of *database.TenantsRepository the bootstrap calls.
type TenantStore interface {
	Create(ctx context.Context, arg database.CreateTenantParams) (database.Tenant, error)
}

// RoleStore is the subset of *database.RBACRepository the bootstrap calls.
type RoleStore interface {
	GetRoleBySlug(ctx context.Context, slug string) (database.Role, error)
}

// MembershipStore is the subset of *database.MembershipsRepository the
// bootstrap calls. The membership is created inside a tenant-scoped context
// built via database.WithTenant.
type MembershipStore interface {
	Create(ctx context.Context, userID uuid.UUID, roleID *uuid.UUID) (database.Membership, error)
}

// Hasher is the subset of *password.Hasher the bootstrap calls.
type Hasher interface {
	Hash(pw string) (string, error)
}

// EnsurePlatformAdmin creates a default tenant + a platform.admin user on
// first run. It is idempotent: when the DB already has any user it returns
// ErrAlreadyPopulated without writing. When Config.Enabled is false or
// AdminEmail is empty it returns ErrDisabled / ErrNoEmail respectively.
//
// On success with Created=true, Result.RawPassword carries the un-hashed
// password (generated when Config.AdminPassword was empty). The caller is
// responsible for logging it ONCE; this package does not log it.
func EnsurePlatformAdmin(ctx context.Context, deps Deps, cfg Config, log *slog.Logger) (Result, error) {
	if log == nil {
		log = slog.Default()
	}
	if !cfg.Enabled {
		return Result{}, ErrDisabled
	}
	if cfg.AdminEmail == "" {
		return Result{}, ErrNoEmail
	}

	// One-shot guard: any non-deleted user means someone has already
	// completed the bootstrap (or seeded a user out-of-band). Either way
	// we do not want to mint a second admin.
	count, err := deps.Users().Count(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("bootstrap: count users: %w", err)
	}
	if count > 0 {
		return Result{}, ErrAlreadyPopulated
	}

	// Resolve the platform.admin role. RBAC seeding runs BEFORE the
	// bootstrap in program.Start, so the row exists; a missing row is a
	// hard error (the seeder would have fail-fast'd, but defensive
	// programming still pays off here).
	role, err := deps.Roles().GetRoleBySlug(ctx, rbac.RolePlatformAdmin)
	if err != nil {
		return Result{}, fmt.Errorf("bootstrap: resolve %s role: %w", rbac.RolePlatformAdmin, err)
	}

	// Create the default tenant the bootstrap admin's membership lands in.
	// The slug/name default to "default" / "Default Tenant" so the operator
	// can rename via the dashboard after first login.
	slug := cfg.DefaultTenantSlug
	if slug == "" {
		slug = "default"
	}
	name := cfg.DefaultTenantName
	if name == "" {
		name = "Default Tenant"
	}
	active := true
	tenant, err := deps.Tenants().Create(ctx, database.CreateTenantParams{
		Slug:     slug,
		Name:     name,
		IsActive: &active,
	})
	if err != nil {
		return Result{}, fmt.Errorf("bootstrap: create default tenant: %w", err)
	}

	// Resolve the password. Generated when the operator did not pre-set one.
	rawPassword := cfg.AdminPassword
	if rawPassword == "" {
		length := cfg.GeneratedPasswordLength
		if length <= 0 {
			length = 24
		}
		gen, genErr := generatePassword(length)
		if genErr != nil {
			return Result{}, fmt.Errorf("bootstrap: generate password: %w", genErr)
		}
		rawPassword = gen
	}
	hash, err := deps.Hasher().Hash(rawPassword)
	if err != nil {
		return Result{}, fmt.Errorf("bootstrap: hash password: %w", err)
	}

	displayName := cfg.AdminDisplayName
	user, err := deps.Users().Create(ctx, database.CreateUserParams{
		Email:        cfg.AdminEmail,
		PasswordHash: &hash,
		IsActive:     &active,
		DisplayName:  &displayName,
		Locale:       "en",
	})
	if err != nil {
		return Result{}, fmt.Errorf("bootstrap: create admin user: %w", err)
	}

	// Link the user to the default tenant with the platform.admin role.
	// The membership store reads the tenant id from the context (the same
	// path the request-time middleware uses), so we wrap the ctx here.
	tenantCtx := database.WithTenant(ctx, tenant.ID)
	if _, mErr := deps.Memberships().Create(tenantCtx, user.ID, &role.ID); mErr != nil {
		return Result{}, fmt.Errorf("bootstrap: create admin membership: %w", mErr)
	}

	log.Info("bootstrap: first-run admin created",
		"tenant_id", tenant.ID,
		"user_id", user.ID,
		"email", user.Email,
		"role", rbac.RolePlatformAdmin,
	)

	return Result{
		Created:     true,
		TenantID:    tenant.ID,
		UserID:      user.ID,
		RawPassword: rawPassword,
	}, nil
}

// passwordAlphabet is the url-safe base64 alphabet (without padding). Picked
// so the generated password is unambiguous to copy-paste and survives URL,
// JSON, and YAML transport without escaping.
const passwordAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

// generatePassword returns a cryptographically-random password of the given
// length over the url-safe base64 alphabet. Uses crypto/rand so the value
// is suitable for production credentials.
//
// Note: the modulo bias introduced by `int(b) % len(alphabet)` is ~0.5 bits
// over 64 candidates vs 256 buckets — negligible for a 24-char password
// (~143 bits of entropy) and well below the argon2id hash's own strength.
func generatePassword(length int) (string, error) {
	if length <= 0 {
		return "", errors.New("bootstrap: password length must be positive")
	}
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("bootstrap: read random bytes: %w", err)
	}
	for i, b := range buf {
		buf[i] = passwordAlphabet[int(b)%len(passwordAlphabet)]
	}
	return string(buf), nil
}
