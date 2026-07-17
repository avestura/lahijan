// Package database: integration_test.go is the entry point for the database
// layer's integration suite. It wires the testcontainers Postgres harness from
// database/testutil. All *_test.go files in this package with the `integration`
// build tag share the one container started here.
//
// Run with:  go test -tags integration ./internal/app/lahijan/database/...

//go:build integration

package database_test

import (
	"testing"

	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
)

// TestMain starts the shared Postgres container, applies migrations up, runs
// the package's tests, then runs migrations fully down and terminates the
// container. testutil.Setup owns os.Exit.
func TestMain(m *testing.M) {
	testutil.Setup(m)
}
