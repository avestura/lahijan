// Package incus: operations.go wraps the Incus async-operations API. Long-
// running calls (instance create, image copy, snapshot) return an Operation
// URL; the client polls GetOperation / WaitOperation until it terminates.
package incus

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// GetOperation fetches the current state of an async operation.
func (p *Provider) GetOperation(ctx context.Context, opID string) (*Operation, error) {
	ctx, span := startSpan(ctx, "operation.get")
	defer span.End()
	raw, err := p.do(ctx, "GET", "operations/"+opID, nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var op Operation
	if err := json.Unmarshal(raw, &op); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode operation: %w", err)
	}
	setStatus(span, nil)
	return &op, nil
}

// WaitOperation blocks until the operation terminates (success, failure, or
// cancel). Uses the Incus wait endpoint with a generous internal timeout; the
// caller's context still applies for cancellation.
//
// The Incus REST API: GET /1.0/operations/<uuid>/wait?timeout=<secs>. Returns
// the final Operation state.
func (p *Provider) WaitOperation(ctx context.Context, opID string) (*Operation, error) {
	ctx, span := startSpan(ctx, "operation.wait",
		opIDAttr(opID))
	defer span.End()

	// Compute a wait timeout from the caller's deadline (if any). Fall back
	// to defaultOperationWaitTimeout when the caller has no deadline.
	waitSecs := int(defaultOperationWaitTimeout.Seconds())
	if dl, ok := ctx.Deadline(); ok {
		// Reserve a small grace period so the wait returns before the caller
		// context expires; the daemon-side wait then surfaces a useful
		// timeout instead of a client-side cancellation.
		if d := time.Until(dl) - 2*time.Second; d > 0 {
			waitSecs = int(d.Seconds())
		}
	}

	path := fmt.Sprintf("operations/%s/wait?timeout=%d", opID, waitSecs)
	// Not p.do: its per-request timeout (30s default) would cut the wait
	// off long before the daemon-side timeout, failing every slow create.
	// Allow the daemon-side wait plus a little slack to deliver the result.
	raw, err := p.doWithTimeout(ctx, time.Duration(waitSecs)*time.Second+10*time.Second, "GET", path, nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}

	// Incus' wait endpoint returns one of two envelopes on completion:
	//
	//   - Operation succeeded: {"type":"async","operation":"...","metadata":{<Operation>}}
	//   - Operation failed:     {"type":"error","error_code":500,"error":"...","metadata":null}
	//
	// The second case still arrives over HTTP 200 (the wait itself
	// succeeded — it's the waited-ON operation that failed). Surface the
	// failure as a Go error so callers (CreateInstance, etc.) propagate it
	// instead of silently swallowing an empty Operation.
	var probe struct {
		Type      string `json:"type"`
		ErrorCode int    `json:"error_code"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(raw, &probe); err == nil && probe.Type == "error" && probe.Error != "" {
		setStatus(span, fmt.Errorf("incus: %s", probe.Error))
		return nil, fmt.Errorf("%w: %s: %s", ErrAsyncOperationFailed, opID, probe.Error)
	}

	// WaitOperation returns a Response whose Metadata is the Operation. Some
	// Incus versions wrap an extra Response envelope around it.
	var op Operation
	if err := json.Unmarshal(raw, &op); err != nil || op.ID == "" {
		// Fall back: try to unwrap a nested envelope.
		var inner Response
		if err := json.Unmarshal(raw, &inner); err == nil && inner.Metadata != nil {
			_ = json.Unmarshal(inner.Metadata, &op)
		}
	}
	// p.do already stripped the response envelope, so a failed operation
	// arrives here as metadata with status "Failure" (400) or "Cancelled"
	// (401) and the reason in err — surface it instead of returning the
	// operation as if it had succeeded.
	if err := operationError(&op); err != nil {
		setStatus(span, err)
		return &op, err
	}
	setStatus(span, nil)
	return &op, nil
}

// operationError returns a non-nil error when op finished unsuccessfully.
func operationError(op *Operation) error {
	if op.StatusCode < 400 && op.Status != "Failure" && op.Status != "Cancelled" {
		return nil
	}
	reason := op.Err
	if reason == "" {
		reason = op.Status
	}
	return fmt.Errorf("%w: %s: %s", ErrAsyncOperationFailed, op.ID, reason)
}

// CancelOperation requests that the daemon cancel a running async operation.
// The daemon may not honour the request for operations that have already
// passed the point of no return (e.g. an in-flight migration); the returned
// Operation reflects the post-cancel state.
func (p *Provider) CancelOperation(ctx context.Context, opID string) error {
	ctx, span := startSpan(ctx, "operation.cancel", opIDAttr(opID))
	defer span.End()
	_, err := p.do(ctx, "DELETE", "operations/"+opID, nil)
	setStatus(span, err)
	return err
}
