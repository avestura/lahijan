// Package database: users_repo.go wraps the sqlc-generated user queries.
// `users` is a global table.
package database

import (
	"context"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// UsersRepository is the persistence boundary for the users table.
type UsersRepository struct {
	q *gen.Queries
}

// NewUsersRepository wraps the given sqlc queries.
func NewUsersRepository(q *gen.Queries) *UsersRepository {
	return &UsersRepository{q: q}
}

// CreateUserParams carries the user-controlled fields of a new user.
// PasswordHash is nil for OAuth/SSO-only users (ADR-0004).
type CreateUserParams struct {
	Email        string
	PasswordHash *string
	IsActive     *bool
}

// Create inserts a user row.
func (r *UsersRepository) Create(ctx context.Context, arg CreateUserParams) (gen.User, error) {
	return r.q.CreateUser(ctx, gen.CreateUserParams{
		Email:        arg.Email,
		PasswordHash: arg.PasswordHash,
		IsActive:     boolOr(arg.IsActive, true),
	})
}

// GetByID returns the user with the given id.
func (r *UsersRepository) GetByID(ctx context.Context, id uuid.UUID) (gen.User, error) {
	return r.q.GetUserByID(ctx, id)
}

// GetByEmail returns the non-deleted user with the given email.
func (r *UsersRepository) GetByEmail(ctx context.Context, email string) (gen.User, error) {
	return r.q.GetUserByEmail(ctx, email)
}

// List returns a page of non-deleted users, newest first.
func (r *UsersRepository) List(ctx context.Context, limit, offset int32) ([]gen.User, error) {
	return r.q.ListUsers(ctx, gen.ListUsersParams{Limit: limit, Offset: offset})
}

// Count returns the number of non-deleted users.
func (r *UsersRepository) Count(ctx context.Context) (int64, error) {
	return r.q.CountUsers(ctx)
}

// UpdatePassword sets a user's password_hash (already an argon2id hash at this
// layer; hashing is the caller's responsibility — see auth in WS-06).
func (r *UsersRepository) UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
	return r.q.UpdateUserPassword(ctx, gen.UpdateUserPasswordParams{ID: id, PasswordHash: &passwordHash})
}

// SoftDelete marks a user deleted and inactive.
func (r *UsersRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	return r.q.SoftDeleteUser(ctx, id)
}
