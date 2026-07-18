// Package database: errors.go holds the package-level helpers for translating
// pgx/sqlc errors into the conventions every service uses.
//
// pgx.ErrNoRows is the only "soft" error this layer surfaces — a query that
// returns no row is not necessarily a failure (often it just means "not
// found", which the caller maps to its own sentinel). Centralising the check
// here keeps the rest of the codebase from importing pgx directly.
//
// ErrNoTenantInContext is defined in pool.go (it has historical ties to the
// pool/bootstrap path); this file re-exports nothing for it, just adds the
// new helper.
package database

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ErrNoSecretVisible is returned by plugin config reads when the row is
// marked is_secret. Treated the same as "not found" from the caller's
// perspective so the plugin cannot distinguish a secret row from a missing
// one. Wraps pgx.ErrNoRows so callers that use IsNoRows still see it as a
// soft not-found.
var ErrNoSecretVisible = errors.Join(pgx.ErrNoRows, errors.New("plugin_config: row is secret"))

// IsNoRows reports whether err is the pgx/sqlc "no rows in result set"
// error. Callers should treat this as a soft "not found" and translate it to
// their own sentinel (e.g. pat.ErrNotFound) rather than letting it bubble up
// to the API envelope as a 500.
func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// IsUniqueViolation reports whether err is a Postgres unique-violation (SQL
// state 23505). The check is string-based (matches against the error's
// Error() text) so the rest of the codebase does not need to import
// pgconn. This is the same trick auth/session uses for isUniqueViolationEmail;
// centralising it here so both call sites use the same shape.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// 23505 is the SQLSTATE for unique_violation; the constraint name often
	// appears as "uq_<table>_<col>". Either signal is enough.
	return strings.Contains(msg, "23505") || strings.Contains(msg, "unique constraint")
}
