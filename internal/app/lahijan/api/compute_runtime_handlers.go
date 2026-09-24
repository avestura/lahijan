// Package api: compute_runtime_handlers.go serves the live instance view
// (GET /instances/{id}/runtime) and instance logs (GET /instances/{id}/logs
// and /logs/{file}). Both are read-only and gated by compute.instance.read.
package api

import (
	"github.com/gofiber/fiber/v2"
	openapi_types "github.com/oapi-codegen/runtime/types"

	apigen "github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
)

// GetComputeInstanceRuntime handles GET /api/v1/compute/instances/{instanceId}/runtime.
func (s *Server) GetComputeInstanceRuntime(c *fiber.Ctx, instanceID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	rt, err := s.computeSvc.GetInstanceRuntime(c.UserContext(), tid, instanceID)
	if err != nil {
		return mapComputeError(c, err)
	}
	return c.JSON(toComputeInstanceRuntimeDTO(rt.Instance, rt.State))
}

// ListComputeInstanceLogs handles GET /api/v1/compute/instances/{instanceId}/logs.
func (s *Server) ListComputeInstanceLogs(c *fiber.Ctx, instanceID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	names, err := s.computeSvc.ListInstanceLogs(c.UserContext(), tid, instanceID)
	if err != nil {
		return mapComputeError(c, err)
	}
	return c.JSON(apigen.ComputeInstanceLogList{Items: names})
}

// GetComputeInstanceLog handles GET /api/v1/compute/instances/{instanceId}/logs/{logFile}.
func (s *Server) GetComputeInstanceLog(c *fiber.Ctx, instanceID openapi_types.UUID, logFile string) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	body, truncated, err := s.computeSvc.GetInstanceLog(c.UserContext(), tid, instanceID, logFile)
	if err != nil {
		return mapComputeError(c, err)
	}
	return c.JSON(apigen.ComputeInstanceLog{Name: logFile, Content: string(body), Truncated: truncated})
}

// toComputeInstanceRuntimeDTO maps the daemon's instance + state onto the
// API shape. Maps are always non-nil so the client can iterate freely.
func toComputeInstanceRuntimeDTO(inst incus.Instance, st *incus.InstanceState) apigen.ComputeInstanceRuntime {
	out := apigen.ComputeInstanceRuntime{
		Status:          inst.Status,
		StatusCode:      intPtrOrNil(inst.StatusCode),
		Type:            strPtrOrNil(inst.Type),
		Architecture:    strPtrOrNil(inst.Architecture),
		Location:        strPtrOrNil(inst.Location),
		Description:     strPtrOrNil(inst.Description),
		Stateful:        &inst.Stateful,
		Ephemeral:       &inst.Ephemeral,
		Profiles:        nonNilSlice(inst.Profiles),
		Config:          nonNilMap(inst.Config),
		Devices:         nonNilDevices(inst.Devices),
		ExpandedConfig:  nonNilMap(inst.ExpandedConfig),
		ExpandedDevices: nonNilDevices(inst.ExpandedDevices),
		Disks:           map[string]apigen.ComputeInstanceDiskUsage{},
		Networks:        map[string]apigen.ComputeInstanceInterface{},
	}
	if !inst.CreatedAt.IsZero() {
		t := inst.CreatedAt
		out.CreatedAt = &t
	}
	if !inst.LastUsedAt.IsZero() {
		t := inst.LastUsedAt
		out.LastUsedAt = &t
	}
	if st == nil {
		return out
	}
	if st.Status != "" {
		out.Status = st.Status
		out.StatusCode = intPtrOrNil(st.StatusCode)
	}
	pid, procs, cpu := st.Pid, st.Processes, st.CPU.Usage
	out.Pid, out.Processes, out.CpuUsageNanoseconds = &pid, &procs, &cpu
	m := st.Memory
	out.Memory = &apigen.ComputeInstanceMemory{
		Usage: &m.Usage, UsagePeak: &m.UsagePeak, Total: &m.Total,
		SwapUsage: &m.SwapUsage, SwapUsagePeak: &m.SwapUsagePeak,
	}
	for name, d := range st.Disk {
		out.Disks[name] = apigen.ComputeInstanceDiskUsage{Usage: &d.Usage, Total: &d.Total}
	}
	for name, n := range st.Network {
		addrs := make([]apigen.ComputeInstanceAddress, 0, len(n.Addresses))
		for _, a := range n.Addresses {
			addrs = append(addrs, apigen.ComputeInstanceAddress{
				Family: a.Family, Address: a.Address,
				Netmask: strPtrOrNil(a.Netmask), Scope: strPtrOrNil(a.Scope),
			})
		}
		ct := n.Counters
		out.Networks[name] = apigen.ComputeInstanceInterface{
			Type: strPtrOrNil(n.Type), State: strPtrOrNil(n.State),
			Hwaddr: strPtrOrNil(n.Hwaddr), HostName: strPtrOrNil(n.HostName),
			Mtu: intPtrOrNil(n.Mtu), Addresses: addrs,
			BytesReceived: &ct.BytesReceived, BytesSent: &ct.BytesSent,
			PacketsReceived: &ct.PacketsReceived, PacketsSent: &ct.PacketsSent,
			ErrorsReceived: &ct.ErrorsReceived, ErrorsSent: &ct.ErrorsSent,
			PacketsDroppedInbound: &ct.PacketsDroppedInbound, PacketsDroppedOutbound: &ct.PacketsDroppedOutbound,
		}
	}
	return out
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func intPtrOrNil(i int) *int {
	if i == 0 {
		return nil
	}
	return &i
}

func nonNilSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func nonNilDevices(m map[string]map[string]string) map[string]map[string]string {
	if m == nil {
		return map[string]map[string]string{}
	}
	return m
}
