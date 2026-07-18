// Package compute: quota_test.go exercises the quota checker with table-
// driven cases. Pure Go (no DB); the checker walks a usage snapshot the
// caller supplies. The list-configs-from-DB path is covered by the
// service-layer integration tests.
package compute

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestInstanceConfig_VCPUs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cfg  InstanceConfig
		want int
	}{
		{name: "empty", cfg: InstanceConfig{}, want: 1},
		{name: "unset", cfg: InstanceConfig{Config: map[string]string{}}, want: 1},
		{name: "integer", cfg: InstanceConfig{Config: map[string]string{"limits.cpu": "4"}}, want: 4},
		{name: "pinned-list", cfg: InstanceConfig{Config: map[string]string{"limits.cpu": "0,1,2"}}, want: 3},
		{name: "garbage", cfg: InstanceConfig{Config: map[string]string{"limits.cpu": "abc"}}, want: 1},
		{name: "negative", cfg: InstanceConfig{Config: map[string]string{"limits.cpu": "-1"}}, want: 1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.cfg.VCPUs())
		})
	}
}

func TestParseToMiB(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"1024", 0},    // 1024 bytes -> 0 MiB
		{"1048576", 1}, // 1 MiB
		{"1MiB", 1},
		{"4MiB", 4},
		{"4GiB", 4096},
		{"1TiB", 1024 * 1024},
		{"1GB", 953},  // 10^9 bytes -> 953.67 MiB -> truncated to 953
		{"4GB", 3814}, // 4 * 953.67 = 3814
		{"garbage", 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, parseToMiB(tc.in))
		})
	}
}

func TestParseToGiB(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"20GiB", 20},
		{"20480MiB", 20},
		{"1TiB", 1024},
		{"100GB", 93},
		{"garbage", 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, parseToGiB(tc.in))
		})
	}
}

func TestQuotaChecker_CheckNewInstance(t *testing.T) {
	t.Parallel()
	cfg := QuotaConfig{
		MaxInstances: 10,
		MaxVCPUs:     40,
		MaxMemoryMiB: 80 * 1024,
		MaxDiskGiB:   800,
	}
	checker := quotaChecker{cfg: cfg}

	t.Run("fits within quota", func(t *testing.T) {
		t.Parallel()
		current := usage{InstanceCount: 5, VCPUTotal: 20, MemoryMiB: 40 * 1024, DiskGiB: 400}
		newCfg := InstanceConfig{
			Config:  map[string]string{"limits.cpu": "4", "limits.memory": "8GiB"},
			Devices: map[string]map[string]string{"root": {"size": "80GiB"}},
		}
		err := checker.checkNewInstance(context.Background(), uuid.Nil, current, newCfg)
		assert.NoError(t, err)
	})

	t.Run("instance count breach", func(t *testing.T) {
		t.Parallel()
		current := usage{InstanceCount: 10, VCPUTotal: 40, MemoryMiB: 80 * 1024, DiskGiB: 800}
		err := checker.checkNewInstance(context.Background(), uuid.Nil, current, InstanceConfig{})
		assert.Error(t, err)
		assert.True(t, IsQuotaExceeded(err))
	})

	t.Run("vcpu breach", func(t *testing.T) {
		t.Parallel()
		current := usage{InstanceCount: 5, VCPUTotal: 38}
		newCfg := InstanceConfig{Config: map[string]string{"limits.cpu": "4"}}
		err := checker.checkNewInstance(context.Background(), uuid.Nil, current, newCfg)
		assert.Error(t, err)
		assert.True(t, IsQuotaExceeded(err))
	})

	t.Run("memory breach", func(t *testing.T) {
		t.Parallel()
		current := usage{InstanceCount: 5, MemoryMiB: 78 * 1024}
		newCfg := InstanceConfig{Config: map[string]string{"limits.memory": "4GiB"}}
		err := checker.checkNewInstance(context.Background(), uuid.Nil, current, newCfg)
		assert.Error(t, err)
		assert.True(t, IsQuotaExceeded(err))
	})

	t.Run("disk breach", func(t *testing.T) {
		t.Parallel()
		current := usage{InstanceCount: 5, DiskGiB: 780}
		newCfg := InstanceConfig{Devices: map[string]map[string]string{"root": {"size": "100GiB"}}}
		err := checker.checkNewInstance(context.Background(), uuid.Nil, current, newCfg)
		assert.Error(t, err)
		assert.True(t, IsQuotaExceeded(err))
	})

	t.Run("zero cap means no enforcement", func(t *testing.T) {
		t.Parallel()
		zero := quotaChecker{cfg: QuotaConfig{}}
		err := zero.checkNewInstance(context.Background(), uuid.Nil,
			usage{InstanceCount: 100, VCPUTotal: 10000}, InstanceConfig{})
		assert.NoError(t, err, "zero quota config = no enforcement (dev path)")
	})
}

// TestQuotaExceededError_Format asserts the message includes the limit +
// current + requested values so the audit metadata is debuggable.
func TestQuotaExceededError_Format(t *testing.T) {
	t.Parallel()
	e := &QuotaExceededError{Dimension: "instances", Limit: 10, Current: 10, Requested: 11}
	msg := e.Error()
	assert.Contains(t, msg, "instances")
	assert.Contains(t, msg, "limit=10")
	assert.Contains(t, msg, "current=10")
	assert.Contains(t, msg, "requested=11")
}
