package email

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/notify/email/testsmtp"
)

// waitFor is the ceiling for asynchronous SMTP delivery assertions; tests use
// WaitUntilMessageCount which polls, so this only bounds the worst case.
const waitFor = 5 * time.Second

// testServer starts an in-process capture server for the email package's tests.
func testServer(t *testing.T) *testsmtp.Server {
	t.Helper()
	return testsmtp.Start(t)
}

func TestNoopSender_AlwaysSucceeds(t *testing.T) {
	t.Parallel()
	require.NoError(t, NoopSender{}.Send(context.Background(), Email{To: "x@y.z"}))
}

func TestRenderHTML_IncludesLocalisedParts(t *testing.T) {
	t.Parallel()
	html, err := RenderHTML(RenderedEmail{
		Greeting:  "Hello,",
		Body:      "Please verify: https://app.example.com/verify?token=abc",
		Signature: "— The Lahijan team",
		CTALabel:  "Verify email",
		CTALink:   "https://app.example.com/verify?token=abc",
	})
	require.NoError(t, err)
	assert.Contains(t, html, "Hello,")
	assert.Contains(t, html, "Please verify:")
	assert.Contains(t, html, "— The Lahijan team")
	assert.Contains(t, html, `href="https://app.example.com/verify?token=abc"`)
	assert.Contains(t, html, "Verify email")
	assert.Contains(t, html, "<!DOCTYPE html>")
}

func TestRenderHTML_EscapesNothingInBodyBecauseTrusted(t *testing.T) {
	t.Parallel()
	// The body comes from our own i18n bundles (trusted), so an ampersand in a
	// link placeholder is passed through verbatim into the rendered HTML.
	html, err := RenderHTML(RenderedEmail{Body: "a & b"})
	require.NoError(t, err)
	assert.Contains(t, html, "a & b")
}

func TestSMTPSender_DisabledDropsMessage(t *testing.T) {
	t.Parallel()
	s := NewSMTPSender(SMTPConfig{Enabled: false})
	// No server running; disabled sender must not even try.
	require.NoError(t, s.Send(context.Background(), Email{To: "x@y.z", Subject: "s", Body: "b"}))
}

func TestSMTPSender_SendsAndCapturesViaTestServer(t *testing.T) {
	// Not parallel: spins up a real goroutine-based SMTP server.
	srv := testServer(t)
	host, port := srv.HostPort()

	sender := NewSMTPSender(SMTPConfig{
		Enabled:  true,
		Host:     host,
		Port:     port,
		From:     "noreply@example.test",
		FromName: "Lahijan",
	})

	html, err := RenderHTML(RenderedEmail{
		Greeting:  "Hello,",
		Body:      "Welcome! Click below to verify your email.",
		Signature: "— The Lahijan team",
	})
	require.NoError(t, err)

	require.NoError(t, sender.Send(context.Background(), Email{
		To:       "user@example.test",
		Subject:  "Verify your email",
		Body:     "Welcome! Click below to verify your email.",
		HTMLBody: html,
		Locale:   "en",
	}))

	ctx, cancel := context.WithTimeout(context.Background(), waitFor)
	defer cancel()
	require.NoError(t, srv.WaitUntilMessageCount(ctx, 1), "expected exactly one captured message")

	msgs := srv.Messages()
	require.Len(t, msgs, 1)
	raw := string(msgs[0].Data)
	assert.Contains(t, raw, "To: user@example.test")
	assert.Contains(t, raw, "Subject: Verify your email")
	assert.Contains(t, raw, "From: \"Lahijan\" <noreply@example.test>")
	assert.Contains(t, raw, "Content-Type: text/html; charset=utf-8")
	assert.Contains(t, raw, "Content-Language: en")
	assert.True(t, strings.Contains(raw, "Welcome!"), "rendered HTML body should be present")
	assert.Contains(t, raw, "— The Lahijan team")
}
