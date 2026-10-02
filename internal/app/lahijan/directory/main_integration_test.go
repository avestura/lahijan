// main_integration_test.go wires the package-wide testcontainers Postgres for
// the directory integration tests (the scratch variant swaps this file).

//go:build integration && !scratch

package directory

import (
	"testing"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
)

// TestMain starts one testcontainers Postgres for the package.
func TestMain(m *testing.M) { testutil.Setup(m) }

func integrationRepos() *database.Repos { return testutil.Repos() }
