// Package testutil is Lahijan's database integration-test harness.
//
// It spins up a real PostgreSQL 16 container via testcontainers-go, applies
// all golang-migrate migrations up, and hands out a *pgxpool.Pool plus factory
// helpers that every later workstream's integration tests can import.
//
// The harness and factories are gated behind the `integration` build tag (per
// .opencode/skills/testing-conventions/SKILL.md) so `go test ./...` without the
// tag stays fast and hermetic. Run the suite with:
//
//	go test -tags integration ./internal/app/lahijan/database/...
//
// The Postgres container is shared across all tests in a package: each test
// package's TestMain calls Setup once, and every test creates its own tenants
// and users (with UUID-disambiguated slugs/emails) so t.Parallel() is safe.
package testutil
