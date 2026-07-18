// Package database: types.go re-exports the sqlc-generated row types under the
// database package so services never need to import gen directly (ADR-0003: the
// repository layer is the only sanctioned entry point to the DB). These are
// type aliases, not wrappers, so the generated Querier return types flow
// through unchanged.
package database

import "github.com/avestura/lahijan/internal/app/lahijan/database/gen"

// Row type aliases for the sqlc-generated models. Services and tests reference
// these (e.g. database.User) instead of gen.User.
type (
	User                = gen.User
	Tenant              = gen.Tenant
	Session             = gen.Session
	RefreshToken        = gen.RefreshToken
	PersonalAccessToken = gen.PersonalAccessToken
	EmailToken          = gen.EmailToken
	Membership          = gen.Membership
	Role                = gen.Role
	Permission          = gen.Permission
	AuditLog            = gen.AuditLog
	AuditLogOutcome     = gen.AuditLogOutcome
	UserOauthIdentity   = gen.UserOauthIdentity
	UserSamlIdentity    = gen.UserSamlIdentity
)
