// Package database: directory_repo.go wraps the sqlc queries for directory
// connections (LDAP / SAML) and the users + groups imported from them. The
// tables are global (platform-level), so none of these methods read a tenant
// from the context.
package database

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// DirectoryRepository is the persistence boundary for directory_* tables.
type DirectoryRepository struct {
	q *gen.Queries
}

// NewDirectoryRepository wraps the given sqlc queries.
func NewDirectoryRepository(q *gen.Queries) *DirectoryRepository {
	return &DirectoryRepository{q: q}
}

// CreateConnectionParams carries the fields of a new connection. Secret is the
// already-sealed secret (nil when the kind has none).
type CreateConnectionParams struct {
	Kind      string
	Name      string
	Enabled   bool
	Config    json.RawMessage
	Secret    []byte
	CreatedBy *uuid.UUID
}

// CreateConnection inserts a connection.
func (r *DirectoryRepository) CreateConnection(ctx context.Context, p CreateConnectionParams) (gen.DirectoryConnection, error) {
	return r.q.CreateDirectoryConnection(ctx, gen.CreateDirectoryConnectionParams{
		Kind: p.Kind, Name: p.Name, Enabled: p.Enabled, Config: p.Config, SecretEncrypted: p.Secret, CreatedBy: p.CreatedBy,
	})
}

// GetConnection returns one connection.
func (r *DirectoryRepository) GetConnection(ctx context.Context, id uuid.UUID) (gen.DirectoryConnection, error) {
	return r.q.GetDirectoryConnection(ctx, id)
}

// ListConnections returns every connection, newest first.
func (r *DirectoryRepository) ListConnections(ctx context.Context) ([]gen.DirectoryConnection, error) {
	return r.q.ListDirectoryConnections(ctx)
}

// UpdateConnectionParams carries the editable fields. A nil Secret keeps the
// stored one.
type UpdateConnectionParams struct {
	ID      uuid.UUID
	Name    string
	Enabled bool
	Config  json.RawMessage
	Secret  []byte
}

// UpdateConnection updates a connection.
func (r *DirectoryRepository) UpdateConnection(ctx context.Context, p UpdateConnectionParams) (gen.DirectoryConnection, error) {
	return r.q.UpdateDirectoryConnection(ctx, gen.UpdateDirectoryConnectionParams{
		ID: p.ID, Name: p.Name, Enabled: p.Enabled, Config: p.Config, SecretEncrypted: p.Secret,
	})
}

// DeleteConnection removes a connection; its groups and user links cascade.
func (r *DirectoryRepository) DeleteConnection(ctx context.Context, id uuid.UUID) error {
	return r.q.DeleteDirectoryConnection(ctx, id)
}

// SetSyncResult records the outcome of the latest sync.
func (r *DirectoryRepository) SetSyncResult(
	ctx context.Context, id uuid.UUID, status, message string, users, groups int32,
) error {
	return r.q.SetDirectoryConnectionSyncResult(ctx, gen.SetDirectoryConnectionSyncResultParams{
		ID: id, LastSyncStatus: &status, LastSyncMessage: &message, LastSyncUsers: &users, LastSyncGroups: &groups,
	})
}

// UpsertGroup inserts or refreshes a group of a connection.
func (r *DirectoryRepository) UpsertGroup(
	ctx context.Context, connectionID uuid.UUID, externalID, name, description string,
) (gen.DirectoryGroup, error) {
	return r.q.UpsertDirectoryGroup(ctx, gen.UpsertDirectoryGroupParams{
		ConnectionID: connectionID, ExternalID: externalID, Name: name, Description: description,
	})
}

// DeleteStaleGroups removes groups of a connection not in keepExternalIDs.
func (r *DirectoryRepository) DeleteStaleGroups(ctx context.Context, connectionID uuid.UUID, keepExternalIDs []string) error {
	return r.q.DeleteStaleDirectoryGroups(ctx, gen.DeleteStaleDirectoryGroupsParams{
		ConnectionID: connectionID, Column2: keepExternalIDs,
	})
}

// ListGroups returns a page of a connection's groups with member counts.
func (r *DirectoryRepository) ListGroups(
	ctx context.Context, connectionID uuid.UUID, limit, offset int32,
) ([]gen.ListDirectoryGroupsRow, error) {
	return r.q.ListDirectoryGroups(ctx, gen.ListDirectoryGroupsParams{ConnectionID: connectionID, Limit: limit, Offset: offset})
}

// CountGroups returns the number of groups of a connection.
func (r *DirectoryRepository) CountGroups(ctx context.Context, connectionID uuid.UUID) (int64, error) {
	return r.q.CountDirectoryGroups(ctx, connectionID)
}

// ReplaceGroupMembers sets a group's members to exactly userIDs.
func (r *DirectoryRepository) ReplaceGroupMembers(ctx context.Context, groupID uuid.UUID, userIDs []uuid.UUID) error {
	if err := r.q.ClearDirectoryGroupMembers(ctx, groupID); err != nil {
		return err
	}
	for _, uid := range userIDs {
		if err := r.q.AddDirectoryGroupMember(ctx, gen.AddDirectoryGroupMemberParams{GroupID: groupID, UserID: uid}); err != nil {
			return err
		}
	}
	return nil
}

// LinkUser records that a user came from a connection entry.
func (r *DirectoryRepository) LinkUser(ctx context.Context, connectionID, userID uuid.UUID, externalID string) error {
	return r.q.UpsertDirectoryUserLink(ctx, gen.UpsertDirectoryUserLinkParams{
		ConnectionID: connectionID, UserID: userID, ExternalID: externalID,
	})
}

// FindLinkedUser returns the link for a directory entry, if the user was
// imported before.
func (r *DirectoryRepository) FindLinkedUser(ctx context.Context, connectionID uuid.UUID, externalID string) (gen.DirectoryUserLink, error) {
	return r.q.GetDirectoryUserLinkByExternalID(ctx, gen.GetDirectoryUserLinkByExternalIDParams{
		ConnectionID: connectionID, ExternalID: externalID,
	})
}

// GroupsForUser lists the directory groups a user belongs to.
func (r *DirectoryRepository) GroupsForUser(ctx context.Context, userID uuid.UUID) ([]gen.ListDirectoryGroupsForUserRow, error) {
	return r.q.ListDirectoryGroupsForUser(ctx, userID)
}

// GetConnectionByName returns the connection with the given name.
func (r *DirectoryRepository) GetConnectionByName(ctx context.Context, name string) (gen.DirectoryConnection, error) {
	return r.q.GetDirectoryConnectionByName(ctx, name)
}

// RemoveUserFromConnectionGroups clears a user's membership in every group of
// one connection.
func (r *DirectoryRepository) RemoveUserFromConnectionGroups(ctx context.Context, connectionID, userID uuid.UUID) error {
	return r.q.RemoveUserFromConnectionGroups(ctx, gen.RemoveUserFromConnectionGroupsParams{
		ConnectionID: connectionID, UserID: userID,
	})
}

// AddUserToGroup adds a user to one group.
func (r *DirectoryRepository) AddUserToGroup(ctx context.Context, groupID, userID uuid.UUID) error {
	return r.q.AddDirectoryGroupMember(ctx, gen.AddDirectoryGroupMemberParams{GroupID: groupID, UserID: userID})
}
