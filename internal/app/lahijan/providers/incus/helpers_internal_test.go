package incus

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

func TestAPIError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()

	var nilErr *APIError
	assert.Equal(t, "incus: <nil>", nilErr.Error())
	assert.Equal(t, "incus: http 500", (&APIError{StatusCode: 500}).Error())
	assert.Equal(t, "incus: http 400: boom", (&APIError{StatusCode: 400, Message: "boom"}).Error())
	assert.ErrorIs(t, (&APIError{StatusCode: 400}).Unwrap(), ErrOperationFailed)
}

func TestAPIError_Is(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		err    *APIError
		target error
		want   bool
	}{
		{"not found", &APIError{StatusCode: http.StatusNotFound}, ErrNotFound, true},
		{"not found mismatch", &APIError{StatusCode: http.StatusOK}, ErrNotFound, false},
		{"already exists", &APIError{StatusCode: http.StatusConflict, Message: "x already exists"}, ErrAlreadyExists, true},
		{"conflict is not already exists", &APIError{StatusCode: http.StatusConflict, Message: "busy"}, ErrAlreadyExists, false},
		{"conflict", &APIError{StatusCode: http.StatusConflict}, ErrConflict, true},
		{"forbidden", &APIError{StatusCode: http.StatusForbidden}, ErrForbidden, true},
		{"bad request", &APIError{StatusCode: http.StatusBadRequest}, ErrBadRequest, true},
		{"other 4xx is operation failed", &APIError{StatusCode: http.StatusTeapot}, ErrOperationFailed, true},
		{"not found is not operation failed", &APIError{StatusCode: http.StatusNotFound}, ErrOperationFailed, false},
		{"5xx is not operation failed", &APIError{StatusCode: http.StatusInternalServerError}, ErrOperationFailed, false},
		{"unknown target", &APIError{StatusCode: http.StatusNotFound}, errors.New("other"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.err.Is(tc.target))
		})
	}
}

func TestOpIDFromURL(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "abc-123", opIDFromURL("/1.0/operations/abc-123"))
	assert.Equal(t, "", opIDFromURL("no-slash"))
	assert.Equal(t, "", opIDFromURL("/trailing/"))
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "short", truncate("short", 10))
	assert.Equal(t, "abc...(truncated)", truncate("abcdef", 3))
}

func TestShouldRetry(t *testing.T) {
	t.Parallel()

	assert.False(t, shouldRetry(nil))
	assert.False(t, shouldRetry(context.Canceled))
	assert.False(t, shouldRetry(fmt.Errorf("dial: %w", context.DeadlineExceeded)))
	assert.False(t, shouldRetry(&websocket.CloseError{Code: websocket.CloseNormalClosure}))
	assert.True(t, shouldRetry(errors.New("connection refused")))
}

func TestEventDispatcherBackoff(t *testing.T) {
	t.Parallel()

	d := &eventDispatcher{}
	start := time.Now()
	d.backoff(context.Background(), 1, 10*time.Millisecond+time.Second)
	assert.GreaterOrEqual(t, time.Since(start), time.Second)

	// A cancelled context returns immediately, and the cap is honoured.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start = time.Now()
	d.backoff(ctx, 10, 3*time.Second)
	assert.Less(t, time.Since(start), time.Second)
}
