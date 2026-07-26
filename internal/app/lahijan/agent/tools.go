// Package agent: tools.go defines the ToolExecutor seam that turns an
// agent tool call into a real side effect. The production implementation is
// the MCP tool bridge: it maps each Lahijan module action (compute / dns /
// storage / billing / audit) to a tool, enforces RequirePerm against the
// calling user's RBAC context, and dispatches to the existing domain
// services. That bridge is WS-31b; this file ships a StubToolExecutor with
// two sample tools so the full call -> confirm -> result loop is testable
// today.
package agent

import (
	"context"
	"encoding/json"
)

// ToolExecutor runs a tool and reports whether a tool is destructive
// (requires human-in-the-loop confirmation). The service consults
// Destructive to decide whether to park a call as pending or execute it
// inline.
type ToolExecutor interface {
	// Destructive reports whether calling tool requires confirmation.
	Destructive(tool string) bool
	// Execute runs the tool with the given JSON args and returns the JSON
	// result. The args come straight from the agent; implementations MUST
	// validate them before use.
	Execute(ctx context.Context, tool string, args json.RawMessage) (json.RawMessage, error)
	// Describe returns the descriptor for a tool, or ok=false if unknown.
	Describe(tool string) (ToolDescriptor, bool)
	// All returns every descriptor the executor exposes. The service uses
	// this to build the per-turn tool list (after applying the denylist).
	All() []ToolDescriptor
}

// StubToolExecutor exposes two sample tools:
//   - ping: a read-only tool that returns {"ok": true}.
//   - sample.destructive: a write tool that returns {"deleted": true} but
//     requires confirmation (Destructive returns true), so the HITL flow is
//     exercised end to end.
type StubToolExecutor struct{}

var _ ToolExecutor = StubToolExecutor{}

// Destructive implements ToolExecutor.
func (StubToolExecutor) Destructive(tool string) bool { return tool == "sample.destructive" }

// Execute implements ToolExecutor.
func (StubToolExecutor) Execute(_ context.Context, tool string, _ json.RawMessage) (json.RawMessage, error) {
	switch tool {
	case "ping":
		return json.RawMessage(`{"ok":true}`), nil
	case "sample.destructive":
		return json.RawMessage(`{"deleted":true}`), nil
	default:
		return nil, ErrToolNotFound
	}
}

// Describe implements ToolExecutor.
func (StubToolExecutor) Describe(tool string) (ToolDescriptor, bool) {
	switch tool {
	case "ping":
		return ToolDescriptor{
			Name: "ping", Description: "Sample read-only tool; returns {ok: true}.",
		}, true
	case "sample.destructive":
		return ToolDescriptor{
			Name:        "sample.destructive",
			Description: "Sample destructive tool; demonstrates the confirm-before-execute HITL flow.",
			Destructive: true,
		}, true
	}
	return ToolDescriptor{}, false
}

// All implements ToolExecutor.
func (StubToolExecutor) All() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: "ping", Description: "Sample read-only tool; returns {ok: true}."},
		{Name: "sample.destructive", Description: "Sample destructive tool; demonstrates HITL.", Destructive: true},
	}
}
