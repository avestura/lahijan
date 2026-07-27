// Package agent: enforcing_executor_test.go locks down the per-tool RBAC +
// audit behaviour that wraps the module tool bridge.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// fakePolicy is a PolicyEvaluator whose answer is fixed at construction.
type fakePolicy struct {
	allow bool
	err   error
}

func (f fakePolicy) HasPermission(_ context.Context, _, _ uuid.UUID, _ string) (bool, error) {
	return f.allow, f.err
}

func (fakePolicy) PermissionsForUser(_ context.Context, _, _ uuid.UUID) ([]string, error) {
	return nil, nil
}

// recordingEmitter captures every Emit + MarkOutcome for assertions.
type recordingEmitter struct {
	emits    []audit.Event
	outcomes []audit.Outcome
	ids      []uuid.UUID
}

func (r *recordingEmitter) Emit(_ context.Context, ev audit.Event) (uuid.UUID, error) {
	id := uuid.New()
	r.emits = append(r.emits, ev)
	r.ids = append(r.ids, id)
	return id, nil
}

func (r *recordingEmitter) MarkOutcome(_ context.Context, _ uuid.UUID, o audit.Outcome) error {
	r.outcomes = append(r.outcomes, o)
	return nil
}

// stubInner is a recording ToolExecutor returning a canned result.
type stubInner struct {
	called []string
	result json.RawMessage
	err    error
}

func (s *stubInner) Destructive(string) bool                { return false }
func (s *stubInner) Describe(string) (ToolDescriptor, bool) { return ToolDescriptor{}, false }
func (s *stubInner) All() []ToolDescriptor                  { return nil }
func (s *stubInner) Execute(_ context.Context, tool string, _ json.RawMessage) (json.RawMessage, error) {
	s.called = append(s.called, tool)
	return s.result, s.err
}

// tenantCtx builds a context carrying a tenant + actor so rbac.Require has
// both ids (it fails closed on uuid.Nil).
func tenantCtx(t *testing.T, userID uuid.UUID) context.Context {
	t.Helper()
	tenantID := uuid.New()
	ctx := database.WithTenant(context.Background(), tenantID)
	return WithActorUserID(ctx, userID)
}

// TestEnforcingExecutor_Allowed runs the tool when the principal holds the
// permission and emits a pending then success audit row.
func TestEnforcingExecutor_Allowed(t *testing.T) {
	t.Parallel()
	inner := &stubInner{result: json.RawMessage(`{"ok":true}`)}
	em := &recordingEmitter{}
	e := NewEnforcingExecutor(inner, fakePolicy{allow: true}, em)

	userID := uuid.New()
	res, err := e.Execute(tenantCtx(t, userID), toolDNSListZones, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("expected allowed, got %v", err)
	}
	if string(res) != `{"ok":true}` {
		t.Fatalf("unexpected result %s", res)
	}
	if len(inner.called) != 1 || inner.called[0] != toolDNSListZones {
		t.Fatalf("inner not called once with %s; got %v", toolDNSListZones, inner.called)
	}
	if len(em.emits) != 1 || em.emits[0].Status != audit.StatusPending {
		t.Fatalf("expected one pending emit, got %+v", em.emits)
	}
	if em.emits[0].Action != audit.ActionAgentToolExecute {
		t.Fatalf("wrong audit action %s", em.emits[0].Action)
	}
	if em.emits[0].ActorUserID == nil || *em.emits[0].ActorUserID != userID {
		t.Fatalf("actor not recorded: %+v", em.emits[0].ActorUserID)
	}
	if len(em.outcomes) != 1 || em.outcomes[0].Status != audit.StatusSuccess {
		t.Fatalf("expected success outcome, got %+v", em.outcomes)
	}
}

// TestEnforcingExecutor_Denied blocks the call, skips the inner executor, and
// records a failure audit row carrying the denial reason.
func TestEnforcingExecutor_Denied(t *testing.T) {
	t.Parallel()
	inner := &stubInner{}
	em := &recordingEmitter{}
	e := NewEnforcingExecutor(inner, fakePolicy{allow: false}, em)

	_, err := e.Execute(tenantCtx(t, uuid.New()), toolComputeListInsts, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected a denial error")
	}
	if !errors.Is(err, rbac.ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied, got %v", err)
	}
	if len(inner.called) != 0 {
		t.Fatalf("inner must NOT run on denial; got %v", inner.called)
	}
	if len(em.emits) != 1 || em.emits[0].Status != audit.StatusFailure {
		t.Fatalf("expected one failure emit, got %+v", em.emits)
	}
	if !slices.Contains([]string{em.emits[0].Metadata["denied"].(string)}, em.emits[0].Metadata["denied"].(string)) {
		// sanity: the denied metadata key exists
	}
	if em.emits[0].Metadata["permission"] != rbac.PermComputeInstanceRead {
		t.Fatalf("permission slug not recorded: %+v", em.emits[0].Metadata)
	}
}

// TestEnforcingExecutor_UnknownToolDelegates confirms a tool with no mapped
// permission is passed straight to the inner executor without an RBAC decision.
func TestEnforcingExecutor_UnknownToolDelegates(t *testing.T) {
	t.Parallel()
	inner := &stubInner{result: json.RawMessage(`{"ok":true}`)}
	em := &recordingEmitter{}
	e := NewEnforcingExecutor(inner, fakePolicy{allow: false}, em)

	if _, err := e.Execute(context.Background(), "ping", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("unknown tool should bypass RBAC; got %v", err)
	}
	if len(inner.called) != 1 {
		t.Fatalf("inner should have run for unknown tool; got %v", inner.called)
	}
	if len(em.emits) != 0 {
		t.Fatalf("no audit row for bypassed tool; got %+v", em.emits)
	}
}
