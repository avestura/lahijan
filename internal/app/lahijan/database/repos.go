// Package database: repos.go defines the repository layer that every service
// uses to reach Postgres. Each repository embeds the sqlc-generated *gen.Queries
// and adds typed methods; tenant-scoped repositories additionally bake the
// tenant id from the request context into every query (ADR-0002). Services
// never construct a *gen.Queries directly.
package database

import "github.com/avestura/lahijan/internal/app/lahijan/database/gen"

// DBTX is re-exported so callers (and tests) can build repos from a pool or a
// transaction without importing the gen package.
type DBTX = gen.DBTX

// Repos is the aggregate of all repositories. Wire it into services once at
// bootstrap (program.Start) and pass the same instance to every handler.
type Repos struct {
	Tenants     *TenantsRepository
	Users       *UsersRepository
	RBAC        *RBACRepository
	Memberships *MembershipsRepository
	AuditLog    *AuditLogRepository
	Tokens      *TokensRepository
}

// NewRepos builds the aggregate repository from a pool or transaction. The
// same DBTX (typically the *pgxpool.Pool) is shared by every sub-repo.
func NewRepos(db DBTX) *Repos {
	q := gen.New(db)
	return &Repos{
		Tenants:     NewTenantsRepository(q),
		Users:       NewUsersRepository(q),
		RBAC:        NewRBACRepository(q),
		Memberships: NewMembershipsRepository(q),
		AuditLog:    NewAuditLogRepository(q),
		Tokens:      NewTokensRepository(q),
	}
}
