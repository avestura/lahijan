// Package compute: doc.go is a placeholder so the build tag on the
// integration TestMain below has a non-empty comment context. The actual
// wiring lives in integration_test_main.go.

//go:build integration

package compute_test

import (
	"testing"

	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
)

// TestMain starts the testcontainers Postgres harness, runs every test in
// the compute_test package, then tears down + exits. Setup owns os.Exit so
// the package's tests share one container.
func TestMain(m *testing.M) { testutil.Setup(m) }
