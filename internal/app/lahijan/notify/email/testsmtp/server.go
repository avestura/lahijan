// Package testsmtp provides an in-process SMTP capture server for tests that
// need to assert on outbound email without an external container (ADR-0018).
// It speaks SMTP via github.com/emersion/go-smtp and stores every received
// message for later inspection.
package testsmtp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"

	"github.com/emersion/go-smtp"
)

// CapturedMessage is one inbound email as the capture server saw it.
type CapturedMessage struct {
	From string
	To   []string
	Data []byte // raw RFC822 bytes (headers + body)
}

// Server is an in-process SMTP server that records every message it receives.
// Start it once per test, read .Messages(), and call Close() in t.Cleanup.
type Server struct {
	addr     string
	server   *smtp.Server
	mu       sync.Mutex
	messages []CapturedMessage
}

// Start launches a capture server on an ephemeral local port and returns it.
// The server runs in its own goroutine and stops on Close.
func Start(t *testing.T) *Server {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("testsmtp: listen: %v", err)
	}
	addr := listener.Addr().String()

	srv := &Server{addr: addr}
	be := &backend{srv: srv}
	s := smtp.NewServer(be)
	s.Addr = addr
	s.AllowInsecureAuth = true
	srv.server = s

	go func() {
		_ = s.Serve(listener) // returns on Close
	}()

	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

// Addr returns the "host:port" the server is listening on.
func (s *Server) Addr() string { return s.addr }

// HostPort returns the host and port separately, convenient for building an
// SMTPConfig in tests.
func (s *Server) HostPort() (string, int) {
	host, port, err := net.SplitHostPort(s.addr)
	if err != nil {
		return "127.0.0.1", 0
	}
	var p int
	_, _ = fmt.Sscanf(port, "%d", &p)
	return host, p
}

// Messages returns a copy of every captured message, in receive order.
func (s *Server) Messages() []CapturedMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]CapturedMessage, len(s.messages))
	copy(out, s.messages)
	return out
}

// WaitUntilMessageCount blocks until at least n messages have been captured or
// ctx is done. Tests use this instead of time.Sleep since SMTP delivery is
// asynchronous from the caller's perspective.
func (s *Server) WaitUntilMessageCount(ctx context.Context, n int) error {
	for {
		s.mu.Lock()
		count := len(s.messages)
		s.mu.Unlock()
		if count >= n {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("testsmtp: timed out waiting for messages")
		default:
		}
	}
}

// Close stops the server and releases the listener.
func (s *Server) Close() error { return s.server.Close() }

// record appends the captured message (called from the backend under the lock
// holder via the per-session struct).
func (s *Server) record(m CapturedMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, m)
}

// backend implements go-smtp's smtp.Backend; every session hands its received
// message back to the parent Server.
type backend struct {
	srv *Server
}

func (b *backend) NewSession(_ *smtp.Conn) (smtp.Session, error) {
	return &session{srv: b.srv}, nil
}

type session struct {
	srv  *Server
	from string
	to   []string
	data bytes.Buffer
}

func (s *session) Mail(from string, _ *smtp.MailOptions) error {
	s.from = from
	return nil
}

func (s *session) Rcpt(to string, _ *smtp.RcptOptions) error {
	s.to = append(s.to, to)
	return nil
}

func (s *session) Data(r io.Reader) error {
	if _, err := io.Copy(&s.data, r); err != nil {
		return err
	}
	s.srv.record(CapturedMessage{
		From: s.from,
		To:   append([]string(nil), s.to...),
		Data: append([]byte(nil), s.data.Bytes()...),
	})
	return nil
}

func (s *session) Reset() {
	s.from = ""
	s.to = nil
	s.data.Reset()
}
func (s *session) Logout() error { return nil }
