// Package agent: actor.go carries the calling user's identity through the
// harness turn so the tool bridge can enforce per-tool RBAC and scope user-
// specific reads (e.g. billing usage) without changing the ToolExecutor
// interface.
//
// The tenant id already travels in the context via database.WithTenant (the
// repository seam reads it); this file adds only the user id, which the
// EnforcingExecutor + the billing tool need.
package agent

import (
	"context"

	"github.com/google/uuid"
)

type actorCtxKey struct{}

// WithActorUserID returns a copy of ctx carrying the calling user's id. The
// agent Service calls this once before harness.Run so every tool execution
// down the turn resolves the same actor.
func WithActorUserID(ctx context.Context, userID uuid.UUID) context.Context {
	return context.WithValue(ctx, actorCtxKey{}, userID)
}

// actorUserIDFromContext returns the calling user's id, or uuid.Nil when none
// is present (the EnforcingExecutor treats Nil as "deny" via rbac.Require, so
// a missing actor fails closed).
func actorUserIDFromContext(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(actorCtxKey{}).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
}
