// Package incus: exec.go wraps the Incus exec endpoint. The flow is:
//
//  1. POST /1.0/instances/<name>/exec with command + wait-for-websocket=true.
//     The daemon responds with an async operation whose metadata carries the
//     per-fd websocket secrets (fds.0=stdin, fds.1=stdout, fds.2=stderr).
//  2. Open a websocket per fd using its secret. Websocket URL pattern:
//     /1.0/operations/<op-uuid>/websocket?secret=<secret>
//  3. Write to stdin, read from stdout/stderr until they close.
//  4. Wait for the operation to terminate; the exit code is in the operation
//     metadata.
//
// WS-14 (xterm.js console) reuses this; WS-24 (noVNC) does NOT — VM consoles
// use a different protocol.
package incus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// ExecResult is the output of an exec call. Stdout + Stderr are the captured
// bytes; ExitCode is the process exit code (0 on success).
type ExecResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// ExecParams controls an exec call.
type ExecParams struct {
	// Project is the Incus project name (the caller maps tenant -> project).
	Project string

	// Instance is the instance name.
	Instance string

	// Command is the argv to execute. Must be non-empty.
	Command []string

	// Environment is the env vars to set (PATH, HOME, ...).
	Environment map[string]string

	// User / Group are the uid/gid to run as. Zero inherits the instance's
	// default user.
	User  int
	Group int

	// Cwd is the working directory inside the instance.
	Cwd string

	// Stdin is the input bytes sent to the process stdin. May be nil.
	Stdin []byte

	// Timeout caps the whole operation. Zero means use the provider's
	// request timeout.
	Timeout time.Duration
}

// Exec runs a command in an instance and returns the captured stdout/stderr
// + the exit code. It opens a websocket per fd (stdin / stdout / stderr),
// pumps the bytes, and waits for the operation to terminate.
//
// For the interactive console use case (WS-14 xterm.js) callers should open
// the websockets directly and stream bytes both ways; Exec is the "run +
// capture" helper for non-interactive use (one-shot scripts, probes).
func (p *Provider) Exec(ctx context.Context, params ExecParams) (*ExecResult, error) {
	ctx, span := startSpan(ctx, "instance.exec",
		projectAttr(params.Project))
	defer span.End()

	if len(params.Command) == 0 {
		err := errors.New("incus: exec requires a non-empty command")
		setStatus(span, err)
		return nil, err
	}

	callCtx := ctx
	if params.Timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, params.Timeout)
		defer cancel()
	}

	// Step 1: POST /instances/<name>/exec. Get back the operation metadata
	// carrying the per-fd secrets.
	secrets, opID, err := p.execOpen(callCtx, params)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}

	// Step 2: open websockets for stdout + stderr (stdin is optional).
	stdoutCh := make(chan ioReadResult, 1)
	stderrCh := make(chan ioReadResult, 2)

	stdoutSecret, hasStdout := secrets["1"]
	stderrSecret, hasStderr := secrets["2"]
	stdinSecret, hasStdin := secrets["0"]

	if !hasStdout {
		missingErr := errors.New("incus: exec response missing stdout secret")
		setStatus(span, missingErr)
		return nil, missingErr
	}

	if hasStdin {
		go p.execWriteLoop(callCtx, opID, stdinSecret, params.Stdin)
	}
	if hasStdout {
		go p.execReadLoop(callCtx, opID, stdoutSecret, stdoutCh)
	} else {
		close(stdoutCh)
	}
	if hasStderr {
		go p.execReadLoop(callCtx, opID, stderrSecret, stderrCh)
	} else {
		close(stderrCh)
	}

	stdoutRes := <-stdoutCh
	var stderrRes ioReadResult
	if hasStderr {
		stderrRes = <-stderrCh
	}

	// Step 3: wait for the operation to terminate; the exit code is in the
	// operation metadata.
	op, err := p.WaitOperation(callCtx, opID)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	setStatus(span, stdoutRes.err)
	if stderrRes.err != nil {
		setStatus(span, stderrRes.err)
	}

	exitCode := execExitCode(op)
	result := &ExecResult{
		Stdout:   stdoutRes.data,
		Stderr:   stderrRes.data,
		ExitCode: exitCode,
	}
	if stdoutRes.err != nil && !errors.Is(stdoutRes.err, io.EOF) {
		return result, fmt.Errorf("incus: exec stdout: %w", stdoutRes.err)
	}
	return result, nil
}

// ioReadResult carries the bytes read from one websocket fd or the error that
// ended the read loop. The error is usually io.EOF; non-EOF errors are
// surfaced to the caller.
type ioReadResult struct {
	data []byte
	err  error
}

// execOpen POSTs to the exec endpoint and returns the per-fd secrets plus the
// operation id.
func (p *Provider) execOpen(ctx context.Context, params ExecParams) (map[string]string, string, error) {
	body := InstanceExecPost{
		Command:     params.Command,
		Environment: params.Environment,
		WaitForWS:   true,
		User:        params.User,
		Group:       params.Group,
		Cwd:         params.Cwd,
	}
	path := "instances/" + url.QueryEscape(params.Instance) + "/exec?project=" + url.QueryEscape(params.Project)
	raw, err := p.do(ctx, "POST", path, body)
	if err != nil {
		return nil, "", err
	}
	// execOpen returns an async operation whose metadata carries the per-fd
	// secrets. We need to decode the operation itself, not just its metadata.
	var op Operation
	if err := json.Unmarshal(raw, &op); err != nil {
		return nil, "", fmt.Errorf("incus: decode exec operation: %w", err)
	}
	if op.ID == "" {
		return nil, "", fmt.Errorf("incus: exec operation missing id: %s", truncate(string(raw), 256))
	}
	var meta ExecMetadata
	if err := json.Unmarshal(op.Metadata, &meta); err != nil {
		return nil, op.ID, fmt.Errorf("incus: decode exec metadata: %w", err)
	}
	meta.OperationID = op.ID
	return meta.FDs, op.ID, nil
}

// execReadLoop opens a websocket for the given fd secret and reads every
// message until the daemon closes the connection. Sends the accumulated bytes
// (and the terminal error) to out. A websocket close frame (or any read
// error) is treated as end-of-stream: the accumulated bytes are returned with
// a nil error so the caller sees the captured output.
func (p *Provider) execReadLoop(ctx context.Context, opID, secret string, out chan<- ioReadResult) {
	conn, err := p.execDial(ctx, opID, secret)
	if err != nil {
		out <- ioReadResult{err: err}
		return
	}
	defer func() { _ = conn.Close() }()

	var buf strings.Builder
	for {
		if err := ctx.Err(); err != nil {
			out <- ioReadResult{data: []byte(buf.String()), err: err}
			return
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			// io.EOF / websocket close — end of stream. The accumulated
			// bytes are the result; the read error is not surfaced
			// unless the context was cancelled.
			if ctxErr := ctx.Err(); ctxErr != nil {
				out <- ioReadResult{data: []byte(buf.String()), err: ctxErr}
				return
			}
			out <- ioReadResult{data: []byte(buf.String()), err: nil}
			return
		}
		buf.Write(data)
	}
}

// execWriteLoop opens a websocket for the stdin fd secret, writes the input
// bytes, and closes the connection so the process sees EOF on stdin.
func (p *Provider) execWriteLoop(ctx context.Context, opID, secret string, input []byte) {
	conn, err := p.execDial(ctx, opID, secret)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	if len(input) > 0 {
		_ = conn.WriteMessage(websocket.TextMessage, input)
	}
	// Send a close message to indicate EOF on stdin.
	_ = conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}

// execDial opens the websocket for an operation's per-fd secret.
func (p *Provider) execDial(ctx context.Context, opID, secret string) (*websocket.Conn, error) {
	wsURL := p.execWebSocketURL(opID, secret)
	conn, _, err := p.wsDialer.DialContext(ctx, wsURL, http.Header{
		"User-Agent": {userAgent},
	})
	if err != nil {
		return nil, fmt.Errorf("incus: exec websocket: %w", err)
	}
	return conn, nil
}

// execWebSocketURL converts the provider's REST baseURL into the websocket
// URL for an operation fd.
func (p *Provider) execWebSocketURL(opID, secret string) string {
	scheme := "ws"
	host := p.baseURL
	switch {
	case strings.HasPrefix(p.baseURL, "https://"):
		scheme = "wss"
		host = strings.TrimPrefix(p.baseURL, "https://")
	case strings.HasPrefix(p.baseURL, "http://"):
		host = strings.TrimPrefix(p.baseURL, "http://")
	}
	q := url.Values{}
	q.Set("secret", secret)
	return fmt.Sprintf("%s://%s%s/operations/%s/websocket?%s",
		scheme, host, apiVersion, url.PathEscape(opID), q.Encode())
}

// execExitCode extracts the process exit code from the operation metadata.
// Returns 0 if the metadata is missing or malformed — matches the daemon's
// own behaviour.
func execExitCode(op *Operation) int {
	if op == nil || len(op.Metadata) == 0 {
		return 0
	}
	var meta struct {
		Output map[string]any `json:"output"`
	}
	if err := json.Unmarshal(op.Metadata, &meta); err != nil {
		return 0
	}
	if v, ok := meta.Output["return"]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case int64:
			return int(n)
		}
	}
	return 0
}
