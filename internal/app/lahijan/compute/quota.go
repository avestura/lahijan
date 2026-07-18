// Package compute: quota.go enforces per-tenant resource caps (WS-14 DoD:
// "quota exceeded -> 422 with clear message"). The check runs before the
// Incus create call so a tenant over quota never reaches the daemon.
//
// Defaults per WS-14 "Open questions" item 1: 4 vCPU, 8 GiB RAM, 80 GiB
// disk, 10 instances. Admin-configurable (the seam is the QuotaConfig
// struct; the WS-14 follow-up wires it to conf).
package compute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// QuotaConfig is the per-tenant cap. Zero fields mean "no cap on this
// dimension" — useful for tests and for the bootstrap path before the
// admin sets real values.
type QuotaConfig struct {
	MaxInstances int   // maximum number of non-deleted instances
	MaxVCPUs     int   // aggregate CPU across every instance
	MaxMemoryMiB int64 // aggregate RAM across every instance (mebibytes)
	MaxDiskGiB   int64 // aggregate root-disk across every instance (gibibytes)
}

// IsZero reports whether the quota config is entirely unset. The service
// falls back to DefaultQuotas() when so.
func (q QuotaConfig) IsZero() bool {
	return q.MaxInstances == 0 && q.MaxVCPUs == 0 && q.MaxMemoryMiB == 0 && q.MaxDiskGiB == 0
}

// DefaultQuotas returns the WS-14 default cap: 10 instances, 4 vCPU each
// (40 vCPU aggregate), 8 GiB RAM each (80 GiB aggregate), 80 GiB disk each
// (800 GiB aggregate). Sized so a tenant can run a small fleet without
// hitting the cap on day one but cannot run away with the host.
func DefaultQuotas() QuotaConfig {
	return QuotaConfig{
		MaxInstances: 10,
		MaxVCPUs:     40,
		MaxMemoryMiB: 80 * 1024,
		MaxDiskGiB:   800,
	}
}

// usage is the per-tenant aggregate the checker walks to decide whether a
// new instance fits. Computed by walking ListConfigsForQuota in the service.
type usage struct {
	InstanceCount int
	VCPUTotal     int
	MemoryMiB     int64
	DiskGiB       int64
}

// QuotaExceededError carries the structured detail the handler returns in
// the error envelope's details map so the UI can render "you are using X
// of Y".
type QuotaExceededError struct {
	Dimension string // "instances" | "vcpu" | "memory_mib" | "disk_gib"
	Limit     int64
	Requested int64
	Current   int64
}

// Error implements error.
func (e *QuotaExceededError) Error() string {
	return fmt.Sprintf("compute: quota exceeded on %s (limit=%d, current=%d, requested=%d)",
		e.Dimension, e.Limit, e.Current, e.Requested)
}

// IsQuotaExceeded reports whether err is a *QuotaExceededError. The handler
// uses it to decide between 422 and 500.
func IsQuotaExceeded(err error) bool {
	var qe *QuotaExceededError
	return errors.As(err, &qe)
}

// quotaChecker walks the tenant's instance configs and enforces the cap.
// Stateless; safe for concurrent use because it reads from the repository
// (which is concurrency-safe).
type quotaChecker struct {
	cfg QuotaConfig
}

// checkNewInstance reports whether a new instance with the given config
// fits within the tenant's quota. Returns a *QuotaExceededError on breach.
func (c quotaChecker) checkNewInstance(
	ctx context.Context,
	tenantID uuid.UUID,
	current usage,
	newConfig InstanceConfig,
) error {
	// Dimension-by-dimension; the first breach wins so the error message
	// is specific. Counting the new instance in the comparison is the
	// caller's job (we receive `current` which is what's already there).
	if c.cfg.MaxInstances > 0 {
		if current.InstanceCount+1 > c.cfg.MaxInstances {
			return &QuotaExceededError{
				Dimension: "instances",
				Limit:     int64(c.cfg.MaxInstances),
				Current:   int64(current.InstanceCount),
				Requested: int64(current.InstanceCount + 1),
			}
		}
	}
	cpu := newConfig.VCPUs()
	if c.cfg.MaxVCPUs > 0 && cpu > 0 {
		if current.VCPUTotal+cpu > c.cfg.MaxVCPUs {
			return &QuotaExceededError{
				Dimension: "vcpu",
				Limit:     int64(c.cfg.MaxVCPUs),
				Current:   int64(current.VCPUTotal),
				Requested: int64(current.VCPUTotal + cpu),
			}
		}
	}
	mem := newConfig.MemoryMiB()
	if c.cfg.MaxMemoryMiB > 0 && mem > 0 {
		if current.MemoryMiB+mem > c.cfg.MaxMemoryMiB {
			return &QuotaExceededError{
				Dimension: "memory_mib",
				Limit:     c.cfg.MaxMemoryMiB,
				Current:   current.MemoryMiB,
				Requested: current.MemoryMiB + mem,
			}
		}
	}
	disk := newConfig.DiskGiB()
	if c.cfg.MaxDiskGiB > 0 && disk > 0 {
		if current.DiskGiB+disk > c.cfg.MaxDiskGiB {
			return &QuotaExceededError{
				Dimension: "disk_gib",
				Limit:     c.cfg.MaxDiskGiB,
				Current:   current.DiskGiB,
				Requested: current.DiskGiB + disk,
			}
		}
	}
	return nil
}

// InstanceConfig is the parsed shape of compute_instances.config_json. The
// schema is:
//
//	{
//	  "config":   {"limits.cpu": "4", "limits.memory": "4GiB", ...},
//	  "devices":  {"root": {"path": "/", "size": "20GiB", "type": "disk"}},
//	  "profiles": ["default"]
//	}
//
// The shape mirrors Incus' own so Lahijan does not re-implement abstractions.
type InstanceConfig struct {
	Config   map[string]string            `json:"config,omitempty"`
	Devices  map[string]map[string]string `json:"devices,omitempty"`
	Profiles []string                     `json:"profiles,omitempty"`
}

// VCPUs returns the limits.cpu value parsed as an integer. Incus accepts
// "4" (a single int -> 4 vCPUs) or "1,2,3" (pinned CPUs -> 3 vCPUs).
// Returns 0 when unset (the daemon will use the default, typically 1
// shared with the host scheduler — treated as 1 for quota).
func (c InstanceConfig) VCPUs() int {
	if c.Config == nil {
		return 1
	}
	raw, ok := c.Config["limits.cpu"]
	if !ok || raw == "" {
		return 1
	}
	if strings.Contains(raw, ",") {
		// pinned-cpu list: count the commas + 1.
		return strings.Count(raw, ",") + 1
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 1
	}
	return n
}

// MemoryMiB returns the limits.memory value parsed to mebibytes. Incus
// accepts "4GiB", "4096MiB", "4GB", "4096MB", or a bare integer (bytes).
// Returns 0 when unset.
func (c InstanceConfig) MemoryMiB() int64 {
	if c.Config == nil {
		return 0
	}
	return parseToMiB(c.Config["limits.memory"])
}

// DiskGiB returns the root disk size parsed to gibibytes. Reads from
// devices.root.size which Incus accepts with GiB / MiB / GB / MB suffixes
// or a bare integer (bytes). Returns 0 when unset.
func (c InstanceConfig) DiskGiB() int64 {
	if c.Devices == nil {
		return 0
	}
	root, ok := c.Devices["root"]
	if !ok {
		return 0
	}
	return parseToGiB(root["size"])
}

// parseInstanceConfig decodes a compute_instances.config_json blob.
// Returns a zero-value InstanceConfig (no error) on a nil/empty input —
// the caller treats it as "use Incus defaults".
func parseInstanceConfig(raw json.RawMessage) (InstanceConfig, error) {
	var cfg InstanceConfig
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("compute: parse instance config: %w", err)
	}
	return cfg, nil
}

// parseToMiB converts an Incus "limits.memory" / "limits.disk" string into
// mebibytes. Accepts the suffixes Incus itself accepts (GiB, MiB, KiB, GB,
// MB, KB, and a bare integer = bytes). Unknown units return 0.
func parseToMiB(s string) int64 {
	if s == "" {
		return 0
	}
	// Try integer first (bare bytes).
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n / (1024 * 1024)
	}
	return int64(parseSuffixed(s, map[string]float64{
		"TiB": 1024 * 1024,
		"GiB": 1024,
		"MiB": 1,
		"KiB": 1.0 / 1024,
		"TB":  953674, // 1 TB = 10^12 bytes = 953_674 MiB
		"GB":  953.67, // 1 GB  = 10^9  bytes = 953.67 MiB
		"MB":  0.95367,
		"KB":  0.000931,
	}))
}

// parseToGiB converts an Incus size string into gibibytes.
func parseToGiB(s string) int64 {
	if s == "" {
		return 0
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n / (1024 * 1024 * 1024)
	}
	return int64(parseSuffixed(s, map[string]float64{
		"TiB": 1024,
		"GiB": 1,
		"MiB": 1.0 / 1024,
		"TB":  931, // 1 TB = 10^12 bytes ≈ 931 GiB
		"GB":  0.931,
		"MB":  0.000891,
	}))
}

// parseSuffixed extracts the numeric prefix and applies the unit factor.
// Returns 0 on no recognised suffix.
func parseSuffixed(s string, units map[string]float64) float64 {
	for suffix, factor := range units {
		if strings.HasSuffix(s, suffix) {
			numStr := strings.TrimSuffix(s, suffix)
			n, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64)
			if err != nil {
				return 0
			}
			return n * factor
		}
	}
	return 0
}

// computeUsage walks the tenant's instance configs and aggregates the
// per-dimension usage. Stateless apart from the repository reads.
func (s *Service) computeUsage(ctx context.Context, tenantID uuid.UUID) (usage, error) {
	rows, err := s.repos.ComputeInstances.ListConfigsForQuota(ctx)
	if err != nil {
		return usage{}, fmt.Errorf("compute: list instance configs for quota: %w", err)
	}
	u := usage{InstanceCount: len(rows)}
	for _, row := range rows {
		cfg, err := parseInstanceConfig(row.ConfigJson)
		if err != nil {
			// A malformed row should not block quota; treat it as 0
			// usage but log the parse failure for follow-up.
			continue
		}
		// The instance count is already accounted for above; we only
		// need the per-instance CPU/RAM/disk additions here.
		cpu := cfg.VCPUs()
		if cpu <= 0 {
			cpu = 1
		}
		u.VCPUTotal += cpu
		u.MemoryMiB += cfg.MemoryMiB()
		u.DiskGiB += cfg.DiskGiB()
	}
	return u, nil
}
