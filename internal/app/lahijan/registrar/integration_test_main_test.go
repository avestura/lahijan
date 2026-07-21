// Package registrar: integration_test_main_test.go wires the
// testcontainers Postgres harness for the registrar_test package.
// Every test in this package that hits Postgres runs under the
// //go:build integration tag and shares one container via
// testutil.Setup.
//
//go:build integration

package registrar_test

import (
	"testing"

	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
)

// TestMain starts the testcontainers Postgres harness, runs every test
// in the registrar_test package, then tears down + exits.
func TestMain(m *testing.M) { testutil.Setup(m) }
