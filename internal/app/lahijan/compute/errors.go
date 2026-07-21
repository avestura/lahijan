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

// WS-25: snapshot + backup errors.

// ErrSnapshotNotFound is returned when the snapshot does not exist within
// the caller's tenant. The handler maps it to 404 not_found.
var ErrSnapshotNotFound = errors.New("compute: snapshot not found")

// ErrSnapshotNameTaken is returned when a create-snapshot call uses a name
// that's already in use for the instance. The handler maps it to 409 conflict.
var ErrSnapshotNameTaken = errors.New("compute: snapshot name already in use")

// ErrBackupTargetNotFound is returned when the backup target does not exist
// within the caller's tenant. The handler maps it to 404 not_found.
var ErrBackupTargetNotFound = errors.New("compute: backup target not found")

// ErrBackupTargetNameTaken is returned when a create-target call uses a
// name that's already in use within the tenant. 409 conflict.
var ErrBackupTargetNameTaken = errors.New("compute: backup target name already in use")

// ErrBackupNotFound is returned when the backup row does not exist within
// the caller's tenant. 404 not_found.
var ErrBackupNotFound = errors.New("compute: backup not found")

// ErrSnapshotPolicyNotFound is returned when the policy row does not exist
// within the caller's tenant. 404 not_found.
var ErrSnapshotPolicyNotFound = errors.New("compute: snapshot policy not found")

// ErrSnapshotPolicyNameTaken is returned when a create-policy call uses a
// name that's already in use within the tenant. 409 conflict.
var ErrSnapshotPolicyNameTaken = errors.New("compute: snapshot policy name already in use")

// ErrInvalidCadence is returned when a snapshot policy cadence cannot be
// parsed as an ISO 8601 duration. 400 bad_request.
var ErrInvalidCadence = errors.New("compute: invalid cadence (expected ISO 8601 duration like PT1H, P1D, P1W)")

// ErrCryptoRequired is returned when a backup-target write/read path is
// exercised but the service was constructed without an AES-GCM envelope.
// 500 internal (the deployer forgot to wire the encryption key).
var ErrCryptoRequired = errors.New("compute: encryption envelope not wired (backup target secrets require auth.secrets.encryptionKey)")

// WS-30: IP pool + floating IP errors.

// ErrIPPoolNotFound is returned when the pool does not exist. The handler
// maps it to 404 not_found.
var ErrIPPoolNotFound = errors.New("compute: ip pool not found")

// ErrIPPoolNameTaken is returned when a create-pool call uses a name
// already in use. The handler maps it to 409 conflict.
var ErrIPPoolNameTaken = errors.New("compute: ip pool name already in use")

// ErrIPPoolRangeNotFound is returned when the range does not exist within
// the named pool. 404 not_found.
var ErrIPPoolRangeNotFound = errors.New("compute: ip pool range not found")

// ErrIPPoolRangeExists is returned when adding a CIDR already present in
// the pool. 409 conflict.
var ErrIPPoolRangeExists = errors.New("compute: ip pool range already exists")

// ErrIPPoolInactive is returned when a tenant tries to allocate from a
// pool the operator has deactivated. 409 conflict.
var ErrIPPoolInactive = errors.New("compute: ip pool is not active")

// ErrFloatingIPNotFound is returned when the floating IP does not exist
// within the caller's tenant. 404 not_found.
var ErrFloatingIPNotFound = errors.New("compute: floating ip not found")

// ErrIPPoolExhausted is returned when the pool has no free addresses.
// The handler maps it to 409 conflict (the operator must add a range or
// the tenant must release an existing allocation).
var ErrIPPoolExhausted = errors.New("compute: ip pool exhausted (no free addresses)")

// ErrInvalidCIDR is returned when an IP-pool-range add call sends a CIDR
// that cannot be parsed. 400 bad_request.
var ErrInvalidCIDR = errors.New("compute: invalid CIDR (expected canonical form like 203.0.113.0/24)")

// ErrInvalidIPFamily is returned when a CIDR's address family does not
// match the explicitly-supplied family parameter. 400 bad_request.
var ErrInvalidIPFamily = errors.New("compute: address family mismatch between CIDR and family parameter")

// ErrFloatingIPAlreadyAttached is returned when an attach call targets an
// instance that already has a floating IP attached. 409 conflict.
var ErrFloatingIPAlreadyAttached = errors.New("compute: instance already has a floating ip attached")

// ErrFloatingIPNotAttached is returned when a detach call targets a
// floating IP that is not currently attached. 409 conflict.
var ErrFloatingIPNotAttached = errors.New("compute: floating ip is not attached to any instance")
