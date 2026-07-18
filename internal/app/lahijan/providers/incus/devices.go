// Package incus: devices.go provides small helpers over the device
// attach/detach surface. Incus has no separate "devices" REST endpoint;
// devices are part of the instance (or profile) config map. These helpers
// wrap UpdateInstance / UpdateProfile so callers do not need to build the
// full InstancePut themselves.
//
// Device types supported (per Incus docs):
//
//   - nic    — network interface (bridged, macvlan, ipvlan, physical, sriov)
//   - disk   — block device or bind-mount
//   - gpu    — GPU passthrough (blocked by the restricted defaults)
//   - proxy  — TCP/UDP/Unix port forwarder
//   - unix-* — host character/block device passthrough (also blocked)
//   - infiniband, usb — passthrough (also blocked)
//
// The device-name (the map key) is the caller's choice; the Lahijan
// convention is "<type>-<purpose>" (e.g. "nic-eth0", "disk-root", "proxy-ssh").
package incus

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
)

// AttachDevice attaches a device to an instance via UpdateInstance. If a
// device with the same name already exists on the instance it is replaced.
//
// deviceName is the per-instance device key; deviceType is the Incus device
// type ("nic", "disk", "proxy", ...); props is the per-device config map.
func (p *Provider) AttachDevice(ctx context.Context, project, instance, deviceName, deviceType string, props map[string]string) error {
	ctx, span := startSpan(ctx, "device.attach",
		projectAttr(project),
		attribute.String("incus.instance", instance),
		attribute.String("incus.device", deviceName))
	defer span.End()

	inst, err := p.GetInstance(ctx, project, instance)
	if err != nil {
		setStatus(span, err)
		return err
	}
	if inst.Devices == nil {
		inst.Devices = make(map[string]map[string]string, 1)
	}
	propsCopy := make(map[string]string, len(props)+1)
	propsCopy["type"] = deviceType
	for k, v := range props {
		propsCopy[k] = v
	}
	inst.Devices[deviceName] = propsCopy

	_, err = p.UpdateInstance(ctx, project, instance, InstancePut{
		Description: inst.Description,
		Config:      inst.Config,
		Devices:     inst.Devices,
		Profiles:    inst.Profiles,
	})
	setStatus(span, err)
	return err
}

// DetachDevice removes a device from an instance. Returns ErrNotFound (wrapped)
// if the device is not present on the instance.
func (p *Provider) DetachDevice(ctx context.Context, project, instance, deviceName string) error {
	ctx, span := startSpan(ctx, "device.detach",
		projectAttr(project),
		attribute.String("incus.instance", instance),
		attribute.String("incus.device", deviceName))
	defer span.End()

	inst, err := p.GetInstance(ctx, project, instance)
	if err != nil {
		setStatus(span, err)
		return err
	}
	if inst.Devices == nil {
		setStatus(span, nil)
		return fmt.Errorf("incus: device %q: %w", deviceName, ErrNotFound)
	}
	if _, ok := inst.Devices[deviceName]; !ok {
		setStatus(span, nil)
		return fmt.Errorf("incus: device %q: %w", deviceName, ErrNotFound)
	}
	delete(inst.Devices, deviceName)
	_, err = p.UpdateInstance(ctx, project, instance, InstancePut{
		Description: inst.Description,
		Config:      inst.Config,
		Devices:     inst.Devices,
		Profiles:    inst.Profiles,
	})
	setStatus(span, err)
	return err
}

// AttachProfileDevice attaches a device to a profile instead of an instance.
// Useful for the "default profile gets the managed nic + root disk" pattern.
func (p *Provider) AttachProfileDevice(ctx context.Context, project, profile, deviceName, deviceType string, props map[string]string) error {
	ctx, span := startSpan(ctx, "device.attach.profile",
		projectAttr(project),
		attribute.String("incus.profile", profile),
		attribute.String("incus.device", deviceName))
	defer span.End()

	prf, err := p.GetProfile(ctx, project, profile)
	if err != nil {
		setStatus(span, err)
		return err
	}
	if prf.Devices == nil {
		prf.Devices = make(map[string]map[string]string, 1)
	}
	propsCopy := make(map[string]string, len(props)+1)
	propsCopy["type"] = deviceType
	for k, v := range props {
		propsCopy[k] = v
	}
	prf.Devices[deviceName] = propsCopy
	err = p.UpdateProfile(ctx, project, profile, ProfilePut{
		Description: prf.Description,
		Config:      prf.Config,
		Devices:     prf.Devices,
	})
	setStatus(span, err)
	return err
}
