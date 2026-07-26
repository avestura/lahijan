// Package agent: llm_harness_test.go verifies the LLMHarness tool-calling
// flow against a scripted httptest server that speaks the OpenAI streaming
// chat-completions protocol. It locks down the high-bug-density behaviour:
// tools are advertised to the model, streamed tool_call fragments are
// reassembled, the read-only agent loop executes tools and feeds results
// back, and destructive tool requests surface for human-in-the-loop.
package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// scriptServer returns an httptest server whose handler walks a slice of
// scripted SSE responses, one per /chat/completions call. Each script entry is
// written verbatim as the response body. The last received request body is
// captured for assertions.
func scriptServer(t *testing.T, scripts []string) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var bodies []map[string]any
	idx := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)
		w.Header().Set("content-type", "text/event-stream")
		if idx >= len(scripts) {
			t.Fatalf("scriptServer: request %d but only %d scripts provided", idx+1, len(scripts))
		}
		_, _ = io.WriteString(w, scripts[idx])
		idx++
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies
}

// drain reads every event from a harness Run channel into a slice. It fails
// the test if the channel does not close within a short deadline (a stuck
// harness would otherwise hang the test forever).
func drain(t *testing.T, events <-chan Event) []Event {
	t.Helper()
	out := make([]Event, 0, 16)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-time.After(2 * time.Second):
			t.Fatal("drain: timed out waiting for the harness channel to close")
			return out
		}
	}
}

// fakeExec is a recording ToolExecutor used by the harness-loop tests. It
// reports one tool ("boom") as destructive and returns a canned result for
// every read-only call.
type fakeExec struct {
	calls   []string
	results map[string]json.RawMessage
}

func (f *fakeExec) Destructive(tool string) bool { return tool == "boom" }

func (f *fakeExec) Execute(_ context.Context, tool string, _ json.RawMessage) (json.RawMessage, error) {
	f.calls = append(f.calls, tool)
	if r, ok := f.results[tool]; ok {
		return r, nil
	}
	return json.RawMessage(`{"ok":true}`), nil
}

func (f *fakeExec) Describe(string) (ToolDescriptor, bool) { return ToolDescriptor{}, false }
func (f *fakeExec) All() []ToolDescriptor                  { return nil }

// sseFrames is a tiny helper that joins delta payloads into the SSE wire
// format the harness parses (`data: <json>\n\n` ... `data: [DONE]\n\n`).
func sseFrames(deltas ...string) string {
	var b strings.Builder
	for _, d := range deltas {
		b.WriteString("data: ")
		b.WriteString(d)
		b.WriteString("\n\n")
	}
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

// TestLLMHarness_AdvertisesTools confirms the request body sent to the model
// carries the advertised tool descriptors when tools are present.
func TestLLMHarness_AdvertisesTools(t *testing.T) {
	t.Parallel()
	script := sseFrames(
		`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	)
	srv, bodies := scriptServer(t, []string{script})

	h := NewLLMHarness().WithToolExecutor(&fakeExec{})
	events, err := h.Run(context.Background(), RunRequest{
		Provider:    ResolvedProvider{Provider: "openai", APIKey: "k", BaseURL: srv.URL},
		Tools:       []ToolDescriptor{{Name: "dns.list_zones", Description: "list zones"}},
		UserMessage: "hi",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	_ = drain(t, events)

	req := (*bodies)[0]
	tools, ok := req["tools"].([]any)
	if !ok {
		t.Fatalf("request did not include a tools array; body=%v", req)
	}
	if len(tools) != 1 {
		t.Fatalf("want 1 advertised tool, got %d", len(tools))
	}
}

// TestLLMHarness_NoToolsOmitsField confirms that when no tools are advertised
// the request body omits the tools field entirely (some providers reject an
// empty array).
func TestLLMHarness_NoToolsOmitsField(t *testing.T) {
	t.Parallel()
	script := sseFrames(
		`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	)
	srv, bodies := scriptServer(t, []string{script})

	h := NewLLMHarness()
	events, err := h.Run(context.Background(), RunRequest{
		Provider:    ResolvedProvider{Provider: "openai", APIKey: "k", BaseURL: srv.URL},
		UserMessage: "hi",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	_ = drain(t, events)

	if _, present := (*bodies)[0]["tools"]; present {
		t.Fatalf("request included a tools field even though none were advertised: %v", (*bodies)[0])
	}
}

// TestLLMHarness_ToolLoop simulates a model that first requests the
// dns.list_zones tool (streamed across two argument fragments), then on the
// follow-up call produces a grounded text answer. We assert: the harness
// executes the tool exactly once, surfaces the result as an EventToolCall with
// a Result, and finally streams the assistant's text.
func TestLLMHarness_ToolLoop(t *testing.T) {
	t.Parallel()
	first := sseFrames(
		`{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"dns.list_zones","arguments":""}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)
	second := sseFrames(
		`{"choices":[{"index":0,"delta":{"content":"You have 2 zones."},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	)
	srv, _ := scriptServer(t, []string{first, second})

	exec := &fakeExec{results: map[string]json.RawMessage{
		"dns.list_zones": json.RawMessage(`{"count":2,"items":[{"name":"a."},{"name":"b."}]}`),
	}}
	h := NewLLMHarness().WithToolExecutor(exec)

	eventsCh, err := h.Run(context.Background(), RunRequest{
		Provider: ResolvedProvider{Provider: "openai", APIKey: "k", BaseURL: srv.URL},
		Tools: []ToolDescriptor{
			{Name: "dns.list_zones", Description: "list zones"},
		},
		UserMessage: "what dns zones do I have?",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := drain(t, eventsCh)

	// The tool must have been executed exactly once and its result surfaced.
	if len(exec.calls) != 1 || exec.calls[0] != "dns.list_zones" {
		t.Fatalf("expected one dns.list_zones execution, got %v", exec.calls)
	}
	var sawToolResult, sawDone bool
	var text strings.Builder
	for _, ev := range events {
		switch ev.Type {
		case EventText:
			text.WriteString(ev.Text)
		case EventToolCall:
			// Read-only execution => Result must be attached.
			if len(ev.Result) == 0 {
				t.Fatalf("EventToolCall for %s carried no Result", ev.Tool)
			}
			sawToolResult = true
		case EventDone:
			sawDone = true
		}
	}
	if !sawToolResult {
		t.Fatalf("expected an EventToolCall with a result; events=%v", events)
	}
	if !sawDone {
		t.Fatalf("expected the turn to end with EventDone")
	}
	if got := text.String(); !strings.Contains(got, "2 zones") {
		t.Fatalf("expected grounded text 'You have 2 zones.', got %q", got)
	}
}

// TestLLMHarness_DestructiveSurfacesHITL confirms a destructive tool request
// is surfaced as an EventToolCall WITHOUT a Result (so the service parks it for
// confirmation) and the turn ends immediately, with no executor invocation.
func TestLLMHarness_DestructiveSurfacesHITL(t *testing.T) {
	t.Parallel()
	first := sseFrames(
		`{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_9","type":"function","function":{"name":"boom","arguments":"{\"id\":\"x\"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)
	srv, _ := scriptServer(t, []string{first})

	exec := &fakeExec{}
	h := NewLLMHarness().WithToolExecutor(exec)

	eventsCh, err := h.Run(context.Background(), RunRequest{
		Provider: ResolvedProvider{Provider: "openai", APIKey: "k", BaseURL: srv.URL},
		Tools: []ToolDescriptor{
			{Name: "boom", Description: "destructive demo", Destructive: true},
		},
		UserMessage: "delete everything",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := drain(t, eventsCh)

	if len(exec.calls) != 0 {
		t.Fatalf("destructive tool must NOT be executed in-harness; got calls %v", exec.calls)
	}
	var surfaced bool
	for _, ev := range events {
		if ev.Type == EventToolCall && ev.Tool == "boom" {
			if len(ev.Result) > 0 {
				t.Fatalf("destructive EventToolCall must not carry a Result; got %s", ev.Result)
			}
			if string(ev.Args) == "" {
				t.Fatalf("destructive EventToolCall must carry the model's args")
			}
			surfaced = true
		}
	}
	if !surfaced {
		t.Fatalf("expected a destructive EventToolCall; events=%v", events)
	}
}

// TestLLMHarness_NoProvider confirms the harness guides the user to configure
// a provider rather than attempting a network call.
func TestLLMHarness_NoProvider(t *testing.T) {
	t.Parallel()
	h := NewLLMHarness()
	eventsCh, err := h.Run(context.Background(), RunRequest{UserMessage: "hi"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := drain(t, eventsCh)
	if len(events) != 1 || events[0].Type != EventError {
		t.Fatalf("expected a single EventError without a provider, got %v", events)
	}
	if !strings.Contains(events[0].Error, "Agent Settings") {
		t.Fatalf("error should point at Agent Settings, got %q", events[0].Error)
	}
}

// TestParseLimit confirms the optional limit argument is clamped and defaults
// sensibly, since the model can legitimately call a list tool with {} or with
// a malformed payload.
func TestParseLimit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args string
		want int32
	}{
		{"empty", ``, listLimit},
		{"object_no_limit", `{}`, listLimit},
		{"explicit", `{"limit":5}`, 5},
		{"below_min", `{"limit":0}`, 1},
		{"above_max", `{"limit":9999}`, listLimit},
		{"garbage", `not json`, listLimit},
	}
	for _, c := range cases {
		if got := parseLimit(json.RawMessage(c.args)); got != c.want {
			t.Errorf("%s: want %d got %d", c.name, c.want, got)
		}
	}
}
