// harness.go wires the package-wide Postgres container + migrations. Call
// Setup exactly once from a test package's TestMain.

//go:build integration

package testutil

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // migrate pg driver
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcwait "github.com/testcontainers/testcontainers-go/wait"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

const (
	pgImage    = "postgres:16-alpine"
	pgDatabase = "lahijan_test"
	pgUser     = "lahijan"
	pgPassword = "lahijan"
)

var (
	pool      *pgxpool.Pool
	container testcontainers.Container
	connHost  string
	connPort  string
)

// Setup wires the package-wide Postgres container + migrations. Call it
// exactly once from a test package's TestMain:
//
//	func TestMain(m *testing.M) { testutil.Setup(m) }
//
// It starts the container, applies all migrations up, runs the package's tests,
// then terminates the container and exits the process. Setup owns os.Exit.
func Setup(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	c, dsn, err := startPostgres(ctx)
	if err != nil {
		log.Fatalf("testutil: start postgres: %v", err)
	}
	container = c

	p, err := database.NewPoolFromDSN(ctx, dsn)
	if err != nil {
		_ = container.Terminate(ctx)
		log.Fatalf("testutil: build pool: %v", err)
	}
	pool = p

	if err := applyMigrations(dsn); err != nil {
		p.Close()
		_ = container.Terminate(ctx)
		log.Fatalf("testutil: apply migrations: %v", err)
	}

	code := m.Run()

	// Best-effort rollback of every migration so a fresh `go test -tags
	// integration` run also exercises the down direction once.
	_ = migrateDownAll(dsn)
	pool.Close()
	termCtx, termCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer termCancel()
	_ = container.Terminate(termCtx)

	os.Exit(code)
}

// Pool returns the package-wide pool wired by Setup. Panics if Setup was not
// called (which is always a TestMain wiring bug).
func Pool() *pgxpool.Pool {
	if pool == nil {
		panic("testutil.Pool() called before testutil.Setup(); wire Setup in TestMain")
	}
	return pool
}

// Repos returns a *database.Repos backed by the package-wide pool. Use it from
// tests that exercise repository methods.
func Repos() *database.Repos {
	return database.NewRepos(Pool())
}

// DatabaseDSN builds a connection string that targets a different logical
// database inside the shared test container. Tests use it to spin up a
// throwaway database (e.g. to re-run migrations from scratch) without starting
// a second container.
func DatabaseDSN(dbName string) string {
	ensureStarted()
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		pgUser, pgPassword, connHost, connPort, dbName)
}

// ensureStarted panics clearly if the harness was not wired via Setup.
func ensureStarted() {
	if pool == nil {
		panic("testutil: container not started; call testutil.Setup in TestMain")
	}
}

// RandSuffix returns a short unique string suitable for disambiguating database
// or table names across parallel test runs.
func RandSuffix() string {
	return uuid.NewString()[:8]
}

func startPostgres(ctx context.Context) (testcontainers.Container, string, error) {
	req := testcontainers.ContainerRequest{
		Image: pgImage,
		Env: map[string]string{
			"POSTGRES_DB":       pgDatabase,
			"POSTGRES_USER":     pgUser,
			"POSTGRES_PASSWORD": pgPassword,
		},
		ExposedPorts: []string{"5432/tcp"},
		WaitingFor: tcwait.ForAll(
			tcwait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60 * time.Second),
		),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, "", fmt.Errorf("generic container: %w", err)
	}
	host, err := c.Host(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("host: %w", err)
	}
	port, err := c.MappedPort(ctx, "5432")
	if err != nil {
		return nil, "", fmt.Errorf("mapped port: %w", err)
	}
	connHost = host
	connPort = port.Port()
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		pgUser, pgPassword, connHost, connPort, pgDatabase)
	return c, dsn, nil
}

func applyMigrations(dsn string) error {
	src, err := iofs.New(database.MigrationsFS(), "migrations")
	if err != nil {
		return fmt.Errorf("iofs source: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	if err != nil {
		return fmt.Errorf("new migrate: %w", err)
	}
	defer m.Close()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

func migrateDownAll(dsn string) error {
	src, err := iofs.New(database.MigrationsFS(), "migrations")
	if err != nil {
		return err
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	if err != nil {
		return err
	}
	defer m.Close()
	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}
