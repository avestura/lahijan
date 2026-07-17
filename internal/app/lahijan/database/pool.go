// Package database is Lahijan's persistence layer: pgx connection pool, sqlc
// generated query types, and typed repository wrappers that enforce the
// tenant-scoping discipline defined in ADR-0002.
//
// All services outside this package reach the database exclusively through the
// repository wrappers (TenantsRepository, UsersRepository, ...). They never
// call into gen.* directly, and they never construct raw SQL.
package database

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/conf"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNoTenantInContext is returned when a tenant-scoped repository is called
// with a context that does not carry a tenant id. This is almost always a bug
// in middleware wiring; every authenticated request must set the tenant scope.
var ErrNoTenantInContext = errors.New("database: tenant id missing from context")

// NewPool builds and validates a *pgxpool.Pool configured from the `database.*`
// config keys. The pool is ready for use when this returns. Callers should
// defer pool.Close() when shutting down.
//
// The statement timeout is applied per-connection via the pool's runtime
// params so a runaway query cannot hold a connection hostage.
func NewPool(ctx context.Context) (*pgxpool.Pool, error) {
	cfg, err := poolConfigFromConf()
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("database: build pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping: %w", err)
	}
	return pool, nil
}

// NewPoolFromDSN builds a *pgxpool.Pool from an explicit DSN, ignoring the
// `database.*` config keys. Intended for tests (e.g. the testcontainers
// harness) and tooling that targets a non-default database.
func NewPoolFromDSN(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("database: parse dsn: %w", err)
	}
	applyDefaults(cfg)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("database: build pool from dsn: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping: %w", err)
	}
	return pool, nil
}

// DSN builds a libpq/PQ-style connection string from the `database.*` config.
// Exported so that migrate targets and the testutil harness can use the same
// connection description as the live pool.
func DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		conf.GetDatabaseUser(),
		conf.GetDatabasePassword(),
		conf.GetDatabaseHost(),
		conf.GetDatabasePort(),
		conf.GetDatabaseName(),
		conf.GetDatabaseSSLMode(),
	)
}

func poolConfigFromConf() (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(DSN())
	if err != nil {
		return nil, fmt.Errorf("database: parse config dsn: %w", err)
	}

	if mx := conf.GetDatabaseMaxConns(); mx > 0 {
		cfg.MaxConns = int32(mx)
	}
	if mn := conf.GetDatabaseMinConns(); mn > 0 {
		cfg.MinConns = int32(mn)
	}
	if lt := conf.GetDatabaseMaxConnLifetimeSeconds(); lt > 0 {
		cfg.MaxConnLifetime = time.Duration(lt) * time.Second
	}
	if it := conf.GetDatabaseMaxConnIdleSeconds(); it > 0 {
		cfg.MaxConnIdleTime = time.Duration(it) * time.Second
	}
	applyDefaults(cfg)

	if to := conf.GetDatabaseStatementTimeoutMs(); to > 0 {
		// statement_timeout is in milliseconds.
		cfg.ConnConfig.RuntimeParams["statement_timeout"] = strconv.Itoa(to)
	}

	return cfg, nil
}

// applyDefaults fills in safe-ish defaults when the config did not pin a value.
func applyDefaults(cfg *pgxpool.Config) {
	if cfg.MaxConns == 0 {
		cfg.MaxConns = 20
	}
	if cfg.MinConns == 0 {
		cfg.MinConns = 2
	}
	if cfg.MaxConnLifetime == 0 {
		cfg.MaxConnLifetime = time.Hour
	}
	if cfg.MaxConnIdleTime == 0 {
		cfg.MaxConnIdleTime = 5 * time.Minute
	}
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
}
