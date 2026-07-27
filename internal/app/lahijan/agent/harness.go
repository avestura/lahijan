// Package agent: harness.go defines the seam between Lahijan and the AI
// agent runtime (the OpenCode harness, driven via opencode-sdk-go). The
// production implementation owns the OpenCode daemon lifecycle + streams
// session events over the SDK; this file ships a StubHarness so the full
// conversation / streaming / HITL / persistence flow is exercised today
// without a live daemon. Swapping in the real harness is a follow-on
// (WS-31a) and only needs to satisfy this interface.
package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Harness drives one agent turn. Implementations:
//   - receive the conversation + the just-appended user message + the
//     resolved model config (provider/model/decrypted key) + the tool
//     descriptors the agent may call,
//   - return a channel of Event values (text deltas, tool-call requests,
//     completion, or error),
//   - close the channel exactly once when the turn is done or ctx cancels.
//
// The Service consumes the channel, persists side effects (assistant message
// content, tool-call rows + results), and forwards each event to the SSE
// stream. The harness MUST NOT block on tool results: a destructive tool call
// is emitted as an EventToolCall and the turn ends; the user confirms via the
// separate HTTP path and continues the conversation with a follow-up message.
type Harness interface {
	Run(ctx context.Context, req RunRequest) (<-chan Event, error)
}

// RunRequest carries everything the harness needs for one turn.
type RunRequest struct {
	ConversationID uuid.UUID
	TenantID       uuid.UUID
	UserID         uuid.UUID
	// History is the prior context, oldest first, EXCLUDING the user
	// message that triggered this turn (passed separately in UserMessage).
	History []HistoryMessage
	// UserMessage is the prompt that triggered this turn.
	UserMessage string
	// Provider is the resolved effective provider + decrypted API key.
	// Zero-value when no provider is configured (the stub ignores it; a
	// real harness would refuse to run without a key).
	Provider ResolvedProvider
	// Tools is the descriptor list of every tool the agent may call this
	// turn (already filtered through the tenant policy denylist).
	Tools []ToolDescriptor
}

// HistoryMessage is one prior turn fed back to the harness as context.
type HistoryMessage struct {
	Role    string // "user" | "assistant" | "tool"
	Content string
}

// ResolvedProvider is the effective provider/model/key the harness should use.
type ResolvedProvider struct {
	Provider      string
	Model         string
	BaseURL       string
	APIKey        string // decrypted plaintext; never logged
	AdminProvided bool   // true => meter + charge (follow-on); false => BYOK
}

// ToolDescriptor advertises one tool the agent may call.
type ToolDescriptor struct {
	Name        string
	Description string
	// Parameters is an optional JSON Schema describing the tool's arguments
	// (the OpenAI function-calling "parameters" field). When empty the harness
	// advertises a permissive object schema (no required properties).
	Parameters json.RawMessage
	// Destructive tools require human-in-the-loop confirmation: the service
	// parks the call as "pending" until the user approves via the HTTP
	// confirm endpoint.
	Destructive bool
}

// EventType discriminates Event.Type.
type EventType string

const (
	// EventText is an assistant token delta; the service buffers these into
	// the assistant message content and streams them to the client.
	EventText EventType = "text"
	// EventToolCall is the agent requesting a tool invocation. The service
	// decides (via the ToolExecutor) whether the tool is destructive; if so
	// the call is parked as pending (HITL), otherwise it executes inline.
	EventToolCall EventType = "tool_call"
	// EventToolResult is emitted by the service (not the harness) once a
	// tool finishes — either inline or after confirmation. The harness
	// never emits this.
	EventToolResult EventType = "tool_result"
	// EventDone marks the end of a successful turn.
	EventDone EventType = "done"
	// EventError marks an unrecoverable harness error; the turn ends.
	EventError EventType = "error"
)

// Event is one streamed agent turn event. Fields are populated based on Type.
type Event struct {
	Type       EventType       `json:"type"`
	Text       string          `json:"text,omitempty"`
	Tool       string          `json:"tool,omitempty"`
	Args       json.RawMessage `json:"args,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	// Usage carries the model's token accounting for a turn (EventDone only,
	// and only when the provider reports usage). The service meters it into
	// usage_events + the ledger when the model is admin-provided; BYOK usage
	// is unmetered. Nil when the provider did not report usage.
	Usage *TurnUsage `json:"usage,omitempty"`
}

// TurnUsage is the token accounting for one agent turn. The harness sums
// across every model round (the read-only tool loop may issue several) and
// reports the total on the terminal EventDone.
type TurnUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StubHarness is the default Harness used until the OpenCode SDK wiring
// lands. It emits a canned reply and, when the user's prompt looks
// destructive, requests a destructive tool call so the full HITL flow is
// demonstrable in tests + the dashboard without a live daemon. Safe for
// concurrent use (no mutable state).
type StubHarness struct {
	// Delay is per-event spacing; zero means no delay. Tests set a small
	// value to simulate streaming without flakiness.
	Delay time.Duration
}

// ensure the stub satisfies the interface at compile time.
var _ Harness = StubHarness{}

// Run implements Harness by streaming a deterministic, prompt-aware reply.
func (h StubHarness) Run(ctx context.Context, req RunRequest) (<-chan Event, error) {
	out := make(chan Event, 8)
	go func() {
		defer close(out)
		send := func(e Event) bool {
			if h.Delay > 0 {
				select {
				case <-time.After(h.Delay):
				case <-ctx.Done():
					return false
				}
			}
			select {
			case out <- e:
				return true
			case <-ctx.Done():
				return false
			}
		}
		if !send(Event{Type: EventText, Text: "(stub harness) I received: " + req.UserMessage + "\n\n"}) {
			return
		}
		if looksDestructive(req.UserMessage) {
			if !send(Event{Type: EventText, Text: "This looks like a destructive action, so it needs your confirmation before I run it."}) {
				return
			}
			args, _ := json.Marshal(map[string]any{"prompt": req.UserMessage})
			if !send(Event{Type: EventToolCall, Tool: "sample.destructive", Args: args}) {
				return
			}
		} else {
			if !send(Event{Type: EventText, Text: "Running a read-only tool to show the round-trip."}) {
				return
			}
			if !send(Event{Type: EventToolCall, Tool: "ping", Args: json.RawMessage(`{}`)}) {
				return
			}
		}
		_ = send(Event{Type: EventDone})
	}()
	return out, nil
}

// looksDestructive is the stub's heuristic for picking the destructive demo
// tool. The real harness decides tool calls; this only shapes the stub.
func looksDestructive(s string) bool {
	s = strings.ToLower(s)
	for _, kw := range []string{"delete", "remove", "destroy", "drop", "purge", "wipe"} {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

// pausedHarness is a Harness whose Run never produces events; used as the
// zero-value when no harness is configured but the subsystem is "enabled"
// for config/policy edits. Not exported — the service constructs it.
type pausedHarness struct{}

var _ Harness = pausedHarness{}

func (pausedHarness) Run(_ context.Context, _ RunRequest) (<-chan Event, error) {
	// Closed-immediately channel: the service's loop sees no events + no
	// done, finalizes an empty assistant message, and returns. Keeps the
	// streaming path functional without a model backend.
	ch := make(chan Event)
	close(ch)
	return ch, nil
}

// onceVar guards the package-level paused harness so it is allocated lazily
// but only once.
var (
	pausedOnce  sync.Once
	pausedValue Harness
)

func pausedDefault() Harness {
	pausedOnce.Do(func() { pausedValue = pausedHarness{} })
	return pausedValue
}
