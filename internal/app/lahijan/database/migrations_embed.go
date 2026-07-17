// Package database: migrations_embed.go exposes the golang-migrate migration
// files as an embed.FS so integration tests (and any tooling that ships in the
// same binary) can apply them from source without depending on the on-disk
// layout. This file is gated behind the `integration` build tag so the
// migration SQL is not compiled into the production binary.

//go:build integration

package database

import (
	"embed"
	"io/fs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// MigrationsFS returns the embedded migration files rooted at "migrations".
// Consumers (e.g. the testutil harness and golang-migrate's iofs source) sub-
// tree it themselves.
func MigrationsFS() fs.FS { return migrationsFS }
