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

	"github.com/jackc/pgx/v5"
)

// IsNoRows reports whether err is the pgx/sqlc "no rows in result set"
// error. Callers should treat this as a soft "not found" and translate it to
// their own sentinel (e.g. pat.ErrNotFound) rather than letting it bubble up
// to the API envelope as a 500.
func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
