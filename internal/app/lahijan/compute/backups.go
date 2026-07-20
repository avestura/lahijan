// Package compute: backups.go defines the BackupTarget abstraction the
// WS-25 off-host backup pipeline talks to (WS-25 scope item:
// "BackupTarget interface with implementations for S3-compatible, local
// NFS, ssh+rsync"). Three drivers ship in-tree:
//
//   - S3Driver   — any S3-compatible endpoint (includes SeaweedFS, MinIO,
//     AWS S3). Configured via endpoint URL + bucket + region + path-style
//     flag. Credentials are AES-GCM-encrypted at rest.
//   - LocalDriver — a locally-mounted directory the operator provisions
//     (typically an NFS mount). The driver writes files directly; the
//     "kind" is exposed as "nfs" in the API so the UI shows the right
//     shape, but the runtime is plain filesystem IO.
//   - SSHDriver  — rsync over SSH to a remote host. Uses
//     golang.org/x/crypto/ssh for the transport and the rsync binary on
//     the local PATH for the delta protocol. Falls back to SCP if rsync
//     is not on PATH.
//
// All three implementations share the BackupTarget interface so the
// compute.backup.create River worker can drive any target without
// knowing which kind it is. The factory (NewBackupTarget) takes the
// row's kind + config + decrypted-secret JSON and returns the right
// driver. A nil/unknown kind yields ErrUnknownBackupTargetKind so a
// misconfigured row fails the worker loudly.
//
// Secret handling: the per-target credentials (S3 access_key+secret,
// SSH private key, NFS password) live in the
// compute_backup_targets.encrypted_secret_json column, AES-GCM-encrypted
// via the process-wide auth.secrets.encryptionKey envelope. The factory
// takes the DECRYPTED secret JSON; the caller (the service layer) is
// responsible for the decrypt step so the drivers never see the
// envelope, only plaintext.
package compute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// BackupTargetKind enumerates the supported driver kinds. Mirrors the
// database.BackupTargetKind* constants; re-declared here so callers do
// not need to import the database package.
const (
	BackupTargetKindS3  = "s3"
	BackupTargetKindNFS = "nfs"
	BackupTargetKindSSH = "ssh"
)

// ErrUnknownBackupTargetKind is returned by NewBackupTarget when the row's
// kind column does not match any registered driver. The handler maps it
// to 400 bad_request (the row was authored incorrectly).
var ErrUnknownBackupTargetKind = errors.New("compute: unknown backup target kind")

// ErrBackupTargetUnreachable is returned by BackupTarget.Ping + Upload when
// the destination cannot be reached. The worker retries per River's
// standard backoff; persistent failure lands the job in the DLQ.
var ErrBackupTargetUnreachable = errors.New("compute: backup target unreachable")

// BackupTarget is the per-driver surface the backup worker talks to.
// Implementations MUST be safe for concurrent use: the worker may run
// several uploads against the same target in parallel.
//
// The interface deliberately does not expose List: the worker only writes
// + deletes; restore reads back via a direct download (Restore method).
type BackupTarget interface {
	// Kind returns the driver's kind slug (matches the row's kind column).
	Kind() string

	// Ping verifies the target is reachable + the credentials work. Called
	// once at factory build time so a misconfigured target fails loudly
	// before the first upload. Returns ErrBackupTargetUnreachable for
	// transport errors.
	Ping(ctx context.Context) error

	// Upload stores body at the given key (a path-like identifier unique
	// within the target). Returns the size in bytes + the SHA-256 checksum
	// the worker records on the backup row. The body is read exactly once;
	// drivers SHOULD stream when the underlying API allows it.
	Upload(ctx context.Context, key string, body io.Reader) (size int64, checksum string, err error)

	// Delete removes the object at key. Idempotent: a missing key is a
	// no-op so retry-on-failure does not double-error.
	Delete(ctx context.Context, key string) error

	// Download fetches the object at key. Used by the restore path. The
	// caller owns the returned reader and MUST Close it.
	Download(ctx context.Context, key string) (io.ReadCloser, error)
}

// BackupTargetConfig is the per-kind config envelope carried in
// compute_backup_targets.config_json. The non-sensitive fields live here;
// the sensitive bits (access keys, SSH keys, NFS password) live in the
// separately-encrypted BackupTargetSecret envelope.
//
// Only the fields relevant to the row's Kind are populated; the others
// stay zero. The factory validates the kind-relevant subset.
type BackupTargetConfig struct {
	// S3 fields (Kind == "s3").
	S3Endpoint       string `json:"endpoint,omitempty"`
	S3Bucket         string `json:"bucket,omitempty"`
	S3Region         string `json:"region,omitempty"`
	S3Prefix         string `json:"prefix,omitempty"`
	S3ForcePathStyle bool   `json:"force_path_style,omitempty"`

	// NFS / local-directory fields (Kind == "nfs"). The directory MUST be
	// writable by the Lahijan process; the operator is responsible for
	// mounting the share before enabling the target.
	NFSMountPoint string `json:"mount_point,omitempty"`
	NFSSubPath    string `json:"sub_path,omitempty"`

	// SSH fields (Kind == "ssh").
	SSHHost           string `json:"host,omitempty"`
	SSHPort           int    `json:"port,omitempty"`
	SSHUser           string `json:"user,omitempty"`
	SSHRemotePath     string `json:"remote_path,omitempty"`
	SSHHostKeyFingerprint string `json:"host_key_fingerprint,omitempty"`
}

// BackupTargetSecret is the per-kind secret envelope carried in the
// AES-GCM-encrypted compute_backup_targets.encrypted_secret_json column.
// Only the fields relevant to the row's Kind are populated.
type BackupTargetSecret struct {
	// S3 credentials (Kind == "s3").
	S3AccessKeyID string `json:"s3_access_key_id,omitempty"`
	S3SecretKey   string `json:"s3_secret_key,omitempty"`

	// NFS password (Kind == "nfs"). Empty when the mount is unauthenticated
	// (the operator pre-mounted the share).
	NFSPassword string `json:"nfs_password,omitempty"`

	// SSH private key (Kind == "ssh"). PEM-encoded.
	SSHPrivateKey   string `json:"ssh_private_key,omitempty"`
	SSHPrivateKeyPW string `json:"ssh_private_key_password,omitempty"`
}

// EncodeConfig marshals a BackupTargetConfig to JSON for storage in
// compute_backup_targets.config_json. The caller passes the result to
// the repository's Create / Update method.
func EncodeConfig(c BackupTargetConfig) (json.RawMessage, error) {
	return json.Marshal(c)
}

// DecodeConfig unmarshals the config_json column back into a typed
// BackupTargetConfig. An empty input yields a zero-value config (no error).
func DecodeConfig(raw json.RawMessage) (BackupTargetConfig, error) {
	var out BackupTargetConfig
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return BackupTargetConfig{}, fmt.Errorf("compute: decode backup target config: %w", err)
	}
	return out, nil
}

// EncodeSecret marshals a BackupTargetSecret to JSON ready for AES-GCM
// encryption. The caller encrypts the result before persisting it.
func EncodeSecret(s BackupTargetSecret) ([]byte, error) {
	return json.Marshal(s)
}

// DecodeSecret unmarshals the decrypted secret JSON back into a typed
// BackupTargetSecret. An empty input yields a zero-value secret.
func DecodeSecret(raw []byte) (BackupTargetSecret, error) {
	var out BackupTargetSecret
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return BackupTargetSecret{}, fmt.Errorf("compute: decode backup target secret: %w", err)
	}
	return out, nil
}

// NewBackupTarget builds the right driver for the given kind. cfg carries
// the non-sensitive per-kind config; secret carries the decrypted
// per-kind credentials. Returns ErrUnknownBackupTargetKind for an
// unknown kind so the worker fails loudly instead of silently skipping.
//
// The returned driver has NOT been pinged; the caller SHOULD call Ping
// before relying on it (the factory does not because tests often build
// drivers against unreachable targets to assert error shapes).
func NewBackupTarget(kind string, cfg BackupTargetConfig, secret BackupTargetSecret) (BackupTarget, error) {
	switch kind {
	case BackupTargetKindS3:
		return NewS3Driver(cfg, secret), nil
	case BackupTargetKindNFS:
		return NewLocalDriver(cfg, secret), nil
	case BackupTargetKindSSH:
		return NewSSHDriver(cfg, secret)
	}
	return nil, fmt.Errorf("%w: %s", ErrUnknownBackupTargetKind, kind)
}
