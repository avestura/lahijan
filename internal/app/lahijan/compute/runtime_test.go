package compute

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// TestGetInstanceLog_RejectsUnsafeNames guards the file name before any
// lookup: it is interpolated into the daemon's log path.
func TestGetInstanceLog_RejectsUnsafeNames(t *testing.T) {
	t.Parallel()
	s := &Service{}
	for _, name := range []string{"", "..", "../etc/passwd", "a/b", "x y", "log\x00"} {
		if _, _, err := s.GetInstanceLog(context.Background(), uuid.New(), uuid.New(), name); !errors.Is(err, ErrInvalidLogName) {
			t.Errorf("%q: got %v, want ErrInvalidLogName", name, err)
		}
	}
}

// TestConsoleBuffers covers both daemon behaviours: destructive reads
// (containers: each read returns only new output) accumulate, and
// non-destructive snapshots (each read repeats the previous) do not
// duplicate.
func TestConsoleBuffers(t *testing.T) {
	t.Parallel()
	c := newConsoleBuffers()
	id := uuid.New()
	if got, _ := c.append(id, []byte("boot\n")); string(got) != "boot\n" {
		t.Fatalf("first read: %q", got)
	}
	if got, _ := c.append(id, nil); string(got) != "boot\n" {
		t.Fatalf("empty destructive read must keep history: %q", got)
	}
	if got, _ := c.append(id, []byte("login: ")); string(got) != "boot\nlogin: " {
		t.Fatalf("destructive read appends: %q", got)
	}
	// Non-destructive snapshot: repeats the previous read plus more.
	if got, _ := c.append(id, []byte("login: root\n")); string(got) != "boot\nlogin: root\n" {
		t.Fatalf("snapshot read must not duplicate: %q", got)
	}
}
