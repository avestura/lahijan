// Package compute: runtime.go serves the live, backend-sourced view of an
// instance (state, usage, effective config) and its log files. The stored
// compute_instances row only records what the user supplied at create time,
// so profile-provided devices and every runtime figure must come from the
// daemon.
package compute

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sync"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
)

// ConsoleLogName is the virtual log file backed by the instance's console
// output buffer rather than a file in the daemon's log directory.
const ConsoleLogName = "console.log"

// ErrInvalidLogName is returned for a log file name outside the safe set.
var ErrInvalidLogName = errors.New("compute: invalid log file name")

// ErrLogNotFound is returned when the requested log is not offered.
var ErrLogNotFound = errors.New("compute: log file not found")

var logNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// InstanceRuntime is the live view of one instance.
type InstanceRuntime struct {
	Instance incus.Instance
	// State is nil when the daemon could not report it (e.g. transient
	// error); the static fields above are still useful then.
	State *incus.InstanceState
}

// GetInstanceRuntime returns the daemon's view of the instance: config,
// effective (profile-expanded) config and devices, and runtime state.
func (s *Service) GetInstanceRuntime(ctx context.Context, _ uuid.UUID, instanceID uuid.UUID) (InstanceRuntime, error) {
	row, err := s.instanceRow(ctx, instanceID)
	if err != nil {
		return InstanceRuntime{}, err
	}
	inst, err := s.provider.GetInstance(ctx, row.ProjectName, row.Name)
	if err != nil {
		if errors.Is(err, incus.ErrNotFound) {
			return InstanceRuntime{}, ErrInstanceNotFound
		}
		return InstanceRuntime{}, fmt.Errorf("compute: incus get instance: %w", err)
	}
	out := InstanceRuntime{Instance: *inst}
	if st, errState := s.provider.GetInstanceState(ctx, row.ProjectName, row.Name); errState == nil {
		out.State = st
	}
	return out, nil
}

// ListInstanceLogs returns the log names offered for the instance; the
// console buffer is always included.
func (s *Service) ListInstanceLogs(ctx context.Context, _ uuid.UUID, instanceID uuid.UUID) ([]string, error) {
	row, err := s.instanceRow(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	names, err := s.provider.ListInstanceLogs(ctx, row.ProjectName, row.Name)
	if err != nil {
		if errors.Is(err, incus.ErrNotFound) {
			return nil, ErrInstanceNotFound
		}
		return nil, fmt.Errorf("compute: incus list logs: %w", err)
	}
	if !slices.Contains(names, ConsoleLogName) {
		names = append([]string{ConsoleLogName}, names...)
	}
	return names, nil
}

// GetInstanceLog returns one log (tailed to 1 MiB) and whether it was cut.
func (s *Service) GetInstanceLog(ctx context.Context, tenantID, instanceID uuid.UUID, name string) ([]byte, bool, error) {
	if !logNamePattern.MatchString(name) || name == "." || name == ".." {
		return nil, false, ErrInvalidLogName
	}
	row, err := s.instanceRow(ctx, instanceID)
	if err != nil {
		return nil, false, err
	}
	if name == ConsoleLogName {
		chunk, _, errConsole := s.provider.GetInstanceConsoleLog(ctx, row.ProjectName, row.Name)
		if errConsole != nil {
			// A stopped container / a VM without a console buffer has
			// nothing new to add; show what was captured so far.
			chunk = nil
		}
		body, truncated := s.consoles.append(instanceID, chunk)
		return body, truncated, nil
	}
	names, err := s.ListInstanceLogs(ctx, tenantID, instanceID)
	if err != nil {
		return nil, false, err
	}
	if !slices.Contains(names, name) {
		return nil, false, ErrLogNotFound
	}
	body, truncated, err := s.provider.GetInstanceLog(ctx, row.ProjectName, row.Name, name)
	if err != nil {
		if errors.Is(err, incus.ErrNotFound) {
			return nil, false, ErrLogNotFound
		}
		return nil, false, fmt.Errorf("compute: incus get log: %w", err)
	}
	return body, truncated, nil
}

// consoleBuffers keeps each instance's console output. Incus clears a
// container's console ring buffer on every read (and refuses to serve the
// on-disk console.log), so without accumulating here the Logs tab showed
// output once and then "(empty)" forever. Per process and in memory: it
// resets on restart and is not shared between replicas.
type consoleBuffers struct {
	mu   sync.Mutex
	byID map[uuid.UUID]*consoleBuffer
}

type consoleBuffer struct {
	acc       []byte // everything captured, tailed to consoleMaxBytes
	last      []byte // the previous raw read, to detect non-destructive reads
	truncated bool
}

// consoleMaxBytes caps the retained console output per instance.
const consoleMaxBytes = 1 << 20

func newConsoleBuffers() *consoleBuffers {
	return &consoleBuffers{byID: map[uuid.UUID]*consoleBuffer{}}
}

// append records one console read and returns the accumulated output. A
// read that starts with the previous read is a non-destructive snapshot
// (it replaces that read instead of duplicating it); anything else is new
// output from a destructive ring-buffer read and is appended.
func (c *consoleBuffers) append(id uuid.UUID, chunk []byte) ([]byte, bool) {
	if c == nil {
		return chunk, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	b := c.byID[id]
	if b == nil {
		b = &consoleBuffer{}
		c.byID[id] = b
	}
	if len(chunk) > 0 {
		if len(b.last) > 0 && bytes.HasPrefix(chunk, b.last) && len(b.acc) >= len(b.last) {
			b.acc = append(b.acc[:len(b.acc)-len(b.last)], chunk...)
		} else {
			b.acc = append(b.acc, chunk...)
		}
		b.last = append(b.last[:0], chunk...)
		if len(b.acc) > consoleMaxBytes {
			b.acc = append([]byte(nil), b.acc[len(b.acc)-consoleMaxBytes:]...)
			b.last = nil // the prefix relationship no longer holds after a trim
			b.truncated = true
		}
	}
	return append([]byte(nil), b.acc...), b.truncated
}

// instanceRow loads the tenant-scoped row (the repo enforces tenant scope).
func (s *Service) instanceRow(ctx context.Context, instanceID uuid.UUID) (database.ComputeInstance, error) {
	if s.provider == nil {
		return database.ComputeInstance{}, ErrProviderDisabled
	}
	row, err := s.repos.ComputeInstances.Get(ctx, instanceID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.ComputeInstance{}, ErrInstanceNotFound
		}
		return database.ComputeInstance{}, fmt.Errorf("compute: get instance: %w", err)
	}
	return row, nil
}
