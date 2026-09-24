package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
)

// TestToComputeInstanceRuntimeDTO covers the fields the Network, Storage,
// Config and Overview tabs render: profile-expanded devices, live
// addresses/counters and disk usage.
func TestToComputeInstanceRuntimeDTO(t *testing.T) {
	t.Parallel()
	inst := incus.Instance{
		Status: "Running", StatusCode: 103, Type: "container", Architecture: "x86_64",
		Profiles:        []string{"default"},
		Config:          map[string]string{"limits.cpu": "1"},
		ExpandedConfig:  map[string]string{"limits.cpu": "1", "limits.memory": "512MiB"},
		ExpandedDevices: map[string]map[string]string{"eth0": {"type": "nic", "network": "lahijanbr"}},
	}
	st := &incus.InstanceState{
		Status: "Running", StatusCode: 103, Pid: 42, Processes: 7,
		CPU:    incus.InstanceStateCPU{Usage: 1_500_000_000},
		Memory: incus.InstanceStateMemory{Usage: 1024, Total: 4096},
		Disk:   map[string]incus.InstanceStateDisk{"root": {Usage: 10, Total: 100}},
		Network: map[string]incus.InstanceStateNetwork{"eth0": {
			Hwaddr: "00:16:3e:00:00:01", State: "up", Type: "broadcast", Mtu: 1500,
			Addresses: []incus.InstanceStateNetworkAddress{{Family: "inet", Address: "10.10.10.5", Netmask: "24", Scope: "global"}},
			Counters:  incus.InstanceStateNetworkCounters{BytesReceived: 5, BytesSent: 6},
		}},
	}
	dto := toComputeInstanceRuntimeDTO(inst, st)
	assert.Equal(t, "Running", dto.Status)
	assert.Equal(t, "lahijanbr", dto.ExpandedDevices["eth0"]["network"])
	assert.Empty(t, dto.Devices, "profile NICs are not local devices")
	assert.NotNil(t, dto.Devices)
	require.Contains(t, dto.Networks, "eth0")
	assert.Equal(t, "10.10.10.5", dto.Networks["eth0"].Addresses[0].Address)
	assert.Equal(t, int64(6), *dto.Networks["eth0"].BytesSent)
	assert.Equal(t, int64(10), *dto.Disks["root"].Usage)
	assert.Equal(t, int64(4096), *dto.Memory.Total)
	assert.Equal(t, int64(1_500_000_000), *dto.CpuUsageNanoseconds)

	stopped := toComputeInstanceRuntimeDTO(incus.Instance{Status: "Stopped"}, nil)
	assert.NotNil(t, stopped.Networks)
	assert.NotNil(t, stopped.ExpandedConfig)
	assert.Nil(t, stopped.Memory)
}
