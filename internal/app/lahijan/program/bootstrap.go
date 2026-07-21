package program

import (
	"context"
	"errors"
	"log/slog"

	"github.com/avestura/lahijan/internal/app/lahijan/bootstrap"
	"github.com/avestura/lahijan/internal/app/lahijan/conf"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// bootstrapDeps adapts *authDeps + *password.Hasher to the bootstrap.Deps
// interface. Lives here (not in the bootstrap package) so the bootstrap
// package stays free of import-time dependencies on conf or auth; tests
// build their own fakes.
type bootstrapDeps struct {
	users       *database.UsersRepository
	tenants     *database.TenantsRepository
	roles       *database.RBACRepository
	memberships *database.MembershipsRepository
	hasher      *bootstrapHasher
}

// bootstrapHasher adapts *password.Hasher to bootstrap.Hasher. We need the
// named type because *password.Hasher has the right method set already, but
// declaring a named adapter here keeps the bootstrap package decoupled
// from the password package at the type level.
type bootstrapHasher struct{ inner passwordHasher }

type passwordHasher interface {
	Hash(pw string) (string, error)
}

func (h *bootstrapHasher) Hash(pw string) (string, error) { return h.inner.Hash(pw) }

func (d *bootstrapDeps) Users() bootstrap.UserStore             { return d.users }
func (d *bootstrapDeps) Tenants() bootstrap.TenantStore         { return d.tenants }
func (d *bootstrapDeps) Roles() bootstrap.RoleStore             { return d.roles }
func (d *bootstrapDeps) Memberships() bootstrap.MembershipStore { return d.memberships }
func (d *bootstrapDeps) Hasher() bootstrap.Hasher               { return d.hasher }

// runBootstrap invokes the first-run admin provisioning flow (WS-23) and
// returns the credentials banner to log at INFO level. The flow is
// idempotent: on any ErrDisabled / ErrNoEmail / ErrAlreadyPopulated path
// it logs at debug and returns silently. A real error returns up to
// program.Start which fail-fast's on it.
//
// The credentials banner is logged via slog at WARN level (it is a
// security-sensitive event the operator must act on) with a clear notice.
// The raw password value travels in the slog attribute "password" so the
// redact handler MAY redact it; deployments that want the literal value
// in `docker compose logs lahijan` can disable redaction for that key.
func runBootstrap(ctx context.Context, deps *authDeps) {
	if deps == nil || deps.repos == nil || deps.hasher == nil {
		return
	}
	cfg := bootstrap.Config{
		Enabled:                 conf.GetBootstrapEnabled(),
		AdminEmail:              conf.GetBootstrapAdminEmail(),
		AdminPassword:           conf.GetBootstrapAdminPassword(),
		AdminDisplayName:        conf.GetBootstrapAdminDisplayName(),
		GeneratedPasswordLength: conf.GetBootstrapGeneratedPasswordLength(),
	}
	if !cfg.Enabled || cfg.AdminEmail == "" {
		slog.Debug("bootstrap: skipped (disabled or adminEmail not set)")
		return
	}

	bd := &bootstrapDeps{
		users:       deps.repos.Users,
		tenants:     deps.repos.Tenants,
		roles:       deps.repos.RBAC,
		memberships: deps.repos.Memberships,
		hasher:      &bootstrapHasher{inner: deps.hasher},
	}
	res, err := bootstrap.EnsurePlatformAdmin(ctx, bd, cfg, slog.Default())
	switch {
	case err == nil:
		// Fall through to the credentials banner below.
	case errors.Is(err, bootstrap.ErrAlreadyPopulated):
		slog.Debug("bootstrap: users already exist; skipping first-run admin")
		return
	case errors.Is(err, bootstrap.ErrDisabled), errors.Is(err, bootstrap.ErrNoEmail):
		// Already gated above; defensive in case EnsurePlatformAdmin's
		// behavior changes.
		slog.Debug("bootstrap: skipped")
		return
	default:
		slog.Error("bootstrap: failed; first-run admin not created", "error", err)
		return
	}

	if !res.Created {
		return
	}

	// Log the credentials banner. WARN level so the operator notices; the
	// "password" attribute carries the raw value exactly once. The notice
	// attributes are stable so log-grep rules can match them.
	slog.Warn(
		"bootstrap: first-run admin credentials (change immediately)",
		"email", cfg.AdminEmail,
		"user_id", res.UserID,
		"tenant_id", res.TenantID,
		"password", res.RawPassword,
		"notice", "rotate this password via the dashboard after first login; it will not be shown again",
	)
}
