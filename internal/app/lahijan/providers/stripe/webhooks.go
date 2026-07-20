// Package stripe: webhooks.go implements Stripe webhook signature
// verification + payload decoding.
//
// Stripe signs every webhook delivery with HMAC-SHA256 using the
// endpoint's `whsec_...` secret. The signature is sent in the
// `Stripe-Signature` header in the format:
//
//	t=<unix_ts>,v1=<hex_hmac>,v0=<...>   (older signatures may carry v0)
//
// We verify by:
//  1. parsing the header into a timestamp + signature list,
//  2. concatenating `timestamp + "." + body`,
//  3. computing HMAC-SHA256(secret, that concat),
//  4. comparing against each v1 signature (constant-time),
//  5. rejecting when the timestamp is older than Config.WebhookTolerance
//     (default 5 minutes) to defend against replays.
//
// The implementation matches Stripe's reference (per the Stripe
// webhooks docs) without pulling in the official SDK.
package stripe

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DefaultWebhookTolerance is the default replay-window size: 5 minutes.
// Stripe sends webhook events near-real-time so anything older is
// almost certainly a replay (or a misconfigured clock).
const DefaultWebhookTolerance = 5 * time.Minute

// MaxWebhookBodyBytes caps the body size we will verify + decode. 1 MiB
// is well above any realistic Stripe event (the largest single event
// Lahijan handles — invoice.paid with a long line-item list — is
// ~30 KB).
const MaxWebhookBodyBytes = 1 * 1024 * 1024

// VerifyWebhook verifies the Stripe-Signature header against the
// supplied body using the supplied secret. Returns the decoded Event
// on success. Returns ErrInvalidSignature on any failure (header
// missing, malformed, signature mismatch, or timestamp outside the
// replay window).
//
// secret is the endpoint's `whsec_...` signing secret. The Provider
// holds a copy at construction; the api handler passes it through so
// this function is independently testable without a Provider.
//
// now is injected so unit tests can run deterministically. Production
// passes time.Now().
func VerifyWebhook(secret, sigHeader string, body []byte, now time.Time, tolerance time.Duration) (Event, error) {
	if secret == "" {
		return Event{}, ErrInvalidSignature
	}
	if len(body) > MaxWebhookBodyBytes {
		return Event{}, fmt.Errorf("%w: body too large", ErrInvalidSignature)
	}
	if tolerance <= 0 {
		tolerance = DefaultWebhookTolerance
	}

	ts, sigs, err := parseSignatureHeader(sigHeader)
	if err != nil {
		return Event{}, err
	}
	// Replay-window check.
	age := now.Sub(time.Unix(ts, 0))
	if age > tolerance || age < -tolerance {
		return Event{}, fmt.Errorf("%w: timestamp outside tolerance (%s)", ErrInvalidSignature, age)
	}

	// Compute the expected signature over "<ts>.<body>".
	payloadToSign := []byte(strconv.FormatInt(ts, 10) + ".")
	payloadToSign = append(payloadToSign, body...)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payloadToSign)
	expected := mac.Sum(nil)

	// Compare against each v1 signature in constant time. Any match is
	// a verified signature.
	matched := false
	for _, sig := range sigs {
		decoded, errDec := hex.DecodeString(sig)
		if errDec != nil {
			continue // malformed; skip
		}
		if hmac.Equal(decoded, expected) {
			matched = true
			break
		}
	}
	if !matched {
		return Event{}, fmt.Errorf("%w: signature mismatch", ErrInvalidSignature)
	}

	// Decode the verified payload into an Event. A decode failure is
	// NOT a signature failure — Stripe might send a payload shape the
	// driver does not know yet. The handler can still acknowledge
	// delivery (return 200) and skip processing.
	var event Event
	if err := jsonUnmarshalEvent(body, &event); err != nil {
		return Event{}, fmt.Errorf("%w: decode payload: %w", ErrInvalidSignature, err)
	}
	return event, nil
}

// parseSignatureHeader parses the `t=...,v1=...,v1=...` header into a
// timestamp + a list of v1 signatures. Returns ErrInvalidSignature on
// any malformation.
func parseSignatureHeader(header string) (ts int64, sigs []string, err error) {
	if header == "" {
		return 0, nil, fmt.Errorf("%w: empty header", ErrInvalidSignature)
	}
	for _, raw := range strings.Split(header, ",") {
		parts := strings.SplitN(strings.TrimSpace(raw), "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "t":
			parsed, errParse := strconv.ParseInt(val, 10, 64)
			if errParse != nil {
				return 0, nil, fmt.Errorf("%w: malformed timestamp", ErrInvalidSignature)
			}
			ts = parsed
		case "v1":
			sigs = append(sigs, val)
		}
	}
	if ts == 0 {
		return 0, nil, fmt.Errorf("%w: missing timestamp", ErrInvalidSignature)
	}
	if len(sigs) == 0 {
		return 0, nil, fmt.Errorf("%w: missing v1 signature", ErrInvalidSignature)
	}
	return ts, sigs, nil
}

// jsonUnmarshalEvent is the JSON decode seam. Defined as a function so
// the test suite can swap in a malformed-payload recorder without
// monkey-patching encoding/json.
var jsonUnmarshalEvent = defaultJSONUnmarshalEvent

func defaultJSONUnmarshalEvent(b []byte, v *Event) error {
	if len(b) == 0 {
		return errors.New("empty body")
	}
	return jsonUnmarshalBytes(b, v)
}
