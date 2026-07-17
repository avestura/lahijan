// Package email provides Lahijan's outbound email subsystem (WS-06): an SMTP
// client wrapper, an HTML wrapper template rendered per locale, and an in-process
// capture server for tests. User-facing strings are produced by i18n.T so every
// message is localizable (ADR-0017).
//
// The Sender interface is the seam every auth flow calls; smtpSender is the
// production implementation (driven by conf.smtp.*) and NoopSender is the
// disabled/test fallback that drops messages on the floor.
package email

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
)

// Email is a single outbound message. Subject and Body are pre-localised by the
// caller via i18n.T; Locale is carried for any header/encoding choice.
type Email struct {
	To       string
	Subject  string
	Body     string // plain-text body with placeholders already interpolated
	HTMLBody string // optional rendered HTML body; falls back to wrapped Body
	Locale   string
}

// Sender delivers an email. Auth flows depend on this interface so tests can
// inject a capturing or no-op sender.
type Sender interface {
	Send(ctx context.Context, msg Email) error
}

// NoopSender accepts every message and drops it. Use it when smtp.enabled is
// false (tests, offline dev).
type NoopSender struct{}

// Send implements Sender by doing nothing.
func (NoopSender) Send(_ context.Context, _ Email) error { return nil }

// SMTPConfig is the subset of conf.smtp.* the sender needs. Build it from conf
// once at bootstrap.
type SMTPConfig struct {
	Enabled  bool
	Host     string
	Port     int
	User     string
	Password string
	From     string
	FromName string
}

// SMTPSender delivers mail via a real SMTP server. When Enabled is false it
// behaves like NoopSender so the caller does not need to branch.
type SMTPSender struct {
	cfg SMTPConfig
}

// NewSMTPSender builds a sender bound to the given config.
func NewSMTPSender(cfg SMTPConfig) *SMTPSender {
	return &SMTPSender{cfg: cfg}
}

// Send delivers msg via SMTP. Auth is PLAIN when a username is configured; an
// empty username targets unauthenticated relays (e.g. MailHog). Returns a
// wrapped error on transport failure so the caller can retry or surface it.
func (s *SMTPSender) Send(_ context.Context, msg Email) error {
	if !s.cfg.Enabled {
		return nil
	}
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	from := formattedFrom(s.cfg.FromName, s.cfg.From)

	var auth smtp.Auth
	if s.cfg.User != "" {
		auth = smtp.PlainAuth("", s.cfg.User, s.cfg.Password, s.cfg.Host)
	}

	body := msg.HTMLBody
	if body == "" {
		body = "<html><body><pre>" + escapeHTML(msg.Body) + "</pre></body></html>"
	}

	raw := buildRFC822(from, msg.To, msg.Subject, body, msg.Locale)
	if err := smtp.SendMail(addr, auth, s.cfg.From, []string{msg.To}, raw); err != nil {
		return fmt.Errorf("email: smtp send to %s: %w", msg.To, err)
	}
	return nil
}

// formattedFrom returns the RFC 5322 From header value, quoting the display name
// when present.
func formattedFrom(name, addr string) string {
	if name == "" {
		return addr
	}
	return fmt.Sprintf("%q <%s>", name, addr)
}

// buildRFC822 assembles the on-the-wire message body that net/smtp.SendMail
// ships. The body is the rendered HTML; Content-Type is text/html; charset
// follows the locale (utf-8 everywhere).
func buildRFC822(from, to, subject, htmlBody, locale string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	if locale != "" {
		fmt.Fprintf(&b, "Content-Language: %s\r\n", locale)
	}
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(htmlBody)
	return []byte(b.String())
}

// escapeHTML is a tiny HTML escaper for the plain-text fallback path. We do not
// pull in html/template for this one-line need; the rendered HTML path goes
// through html/template in templates.go.
func escapeHTML(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	)
	return r.Replace(s)
}
