// Package compute: errors.go holds the sentinel errors the compute service
// surfaces. Handlers translate them to HTTP envelopes via api/errors.go.
package compute

import "errors"

// ErrProviderDisabled is returned when the Incus provider is not configured.
// The handler maps it to 501 not_implemented.
var ErrProviderDisabled = errors.New("compute: provider is not enabled on this server")

// ErrInstanceNotFound is returned when the instance does not exist within
// the caller's tenant. The handler maps it to 404 not_found.
var ErrInstanceNotFound = errors.New("compute: instance not found")

// ErrInstanceNameTaken is returned when a create call uses a name that's
// already in use within the tenant. The handler maps it to 409 conflict.
var ErrInstanceNameTaken = errors.New("compute: instance name is already in use")

// ErrImageNotFound is returned when an image alias / fingerprint cannot be
// resolved within the caller's tenant. The handler maps it to 404 not_found.
var ErrImageNotFound = errors.New("compute: image not found")

// ErrProfileNotFound / ErrNetworkNotFound / ErrVolumeNotFound mirror the
// instance case for the secondary resources.
var (
	ErrProfileNotFound = errors.New("compute: profile not found")
	ErrNetworkNotFound = errors.New("compute: network not found")
	ErrVolumeNotFound  = errors.New("compute: storage volume not found")
)

// ErrNameTaken is the generic 409 for the secondary resources.
var ErrNameTaken = errors.New("compute: name is already in use")

// ErrQuotaExceeded is returned when the create call would push the tenant
// over its quota. The handler maps it to 422 unprocessable_entity.
var ErrQuotaExceeded = errors.New("compute: quota exceeded")

// ErrInsufficientBalance is returned when the create call would push the
// user's balance below zero (ADR-0013). The handler maps it to 402
// payment_required.
var ErrInsufficientBalance = errors.New("compute: insufficient balance")

// ErrInstanceNotRunning is returned when the caller invokes exec against an
// instance that is not in the Running state (per WS-14 open question 3:
// exec is available only on running instances).
var ErrInstanceNotRunning = errors.New("compute: instance must be running to exec")

// ErrInstanceNotVM is returned when the caller invokes the graphical (VNC)
// console flow (WS-24) against an instance that is not a virtual machine.
// Containers do not get a VGA console. The handler maps it to 409 conflict.
var ErrInstanceNotVM = errors.New("compute: graphical console is only available for virtual-machine instances")

// ErrVNCUnavailable is returned when the Incus daemon refuses or fails the
// console-open call for a reason the service does not surface more
// specifically (e.g. the VM's agent is not yet up, the operation timed
// out, the VGA backend is not installed). The handler maps it to 503
// service_unavailable.
var ErrVNCUnavailable = errors.New("compute: graphical console unavailable")

// ErrInvalidName is returned when the caller sends an empty or invalid name.
var ErrInvalidName = errors.New("compute: name is required")

// ErrInvalidImage is returned when the caller sends an empty image alias.
var ErrInvalidImage = errors.New("compute: image alias is required")
