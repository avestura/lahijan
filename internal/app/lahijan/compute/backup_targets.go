// Package compute: backup_targets.go implements the user-facing backup
// target + exported-backup surface (WS-25).
//
// A backup target is an off-host destination for exported snapshots. The
// admin configures one or more per tenant; the backup worker
// (compute.backup.create) pulls a snapshot from Incus + pushes it via the
// BackupTarget driver. Sensitive credentials (S3 access keys, SSH private
// keys, NFS passwords) live AES-GCM-encrypted in
// compute_backup_targets.encrypted_secret_json using the process-wide
// auth.secrets.encryptionKey envelope.
//
// Every privileged action emits an audit row BEFORE the side effect
// (status=pending) and marks the outcome AFTER, mirroring the snapshot +
// instance patterns.
package compute

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
	"github.com/google/uuid"
)

// CreateBackupTargetParams carries the user-controlled fields of a
// backup-target create call. Secret is the plaintext credential envelope
// the service AES-GCM-encrypts before persisting.
type CreateBackupTargetParams struct {
	Name        string
	Kind        string
	Description string
	Config      BackupTargetConfig
	Secret      BackupTargetSecret
	Enabled     bool
}

// validBackupTargetKinds is the allow-list of driver kinds the service
// accepts on create. Adding a driver to backups.go extends this set.
var validBackupTargetKinds = map[string]struct{}{
	BackupTargetKindS3:  {},
	BackupTargetKindNFS: {},
	BackupTargetKindSSH: {},
}

// CreateBackupTarget creates a backup target row + encrypts the secret
// envelope. The Ping runs against the target after the row is persisted so
// a misconfigured target fails the next backup loudly (not the create
// itself; the create persists the config so the operator can fix + retry).
func (s *Service) CreateBackupTarget(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	params CreateBackupTargetParams,
) (database.ComputeBackupTarget, error) {
	if params.Name == "" {
		return database.ComputeBackupTarget{}, ErrInvalidName
	}
	if _, ok := validBackupTargetKinds[params.Kind]; !ok {
		return database.ComputeBackupTarget{}, fmt.Errorf("%w: %s", ErrUnknownBackupTargetKind, params.Kind)
	}
	if s.crypto == nil {
		return database.ComputeBackupTarget{}, ErrCryptoRequired
	}

	// Pre-flight: name uniqueness within the tenant.
	if existing, err := s.repos.ComputeBackupTargets.GetByName(ctx, params.Name); err == nil && existing.ID != uuid.Nil {
		return database.ComputeBackupTarget{}, fmt.Errorf("%w: name=%s", ErrBackupTargetNameTaken, params.Name)
	} else if err != nil && !database.IsNoRows(err) {
		return database.ComputeBackupTarget{}, fmt.Errorf("compute: target lookup name: %w", err)
	}

	cfgJSON, err := EncodeConfig(params.Config)
	if err != nil {
		return database.ComputeBackupTarget{}, fmt.Errorf("compute: encode config: %w", err)
	}
	secretJSON, err := EncodeSecret(params.Secret)
	if err != nil {
		return database.ComputeBackupTarget{}, fmt.Errorf("compute: encode secret: %w", err)
	}
	encryptedSecret, err := s.crypto.Seal(string(secretJSON))
	if err != nil {
		return database.ComputeBackupTarget{}, fmt.Errorf("compute: encrypt secret: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditBackupTargetCreate,
		ResourceType: ResourceBackupTarget,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"name": params.Name,
			"kind": params.Kind,
		},
	})

	row, err := s.repos.ComputeBackupTargets.Create(ctx, database.CreateComputeBackupTargetParams{
		Name:                params.Name,
		Kind:                params.Kind,
		Description:         params.Description,
		Config:              cfgJSON,
		EncryptedSecretJSON: []byte(encryptedSecret),
		Enabled:             params.Enabled,
	})
	if err != nil {
		if database.IsUniqueViolation(err) {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
				"error": "name taken",
			}})
			return database.ComputeBackupTarget{}, fmt.Errorf("%w: name=%s", ErrBackupTargetNameTaken, params.Name)
		}
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return database.ComputeBackupTarget{}, fmt.Errorf("compute: create backup target: %w", err)
	}

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
		"target_id": row.ID,
	}})
	return row, nil
}

// GetBackupTarget returns the target row. The decrypted secret is NOT
// surfaced here; callers that need it (the worker, the update-secret
// path) call DecryptBackupTargetSecret directly.
func (s *Service) GetBackupTarget(
	ctx context.Context,
	_ uuid.UUID,
	targetID uuid.UUID,
) (database.ComputeBackupTarget, error) {
	row, err := s.repos.ComputeBackupTargets.Get(ctx, targetID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.ComputeBackupTarget{}, ErrBackupTargetNotFound
		}
		return database.ComputeBackupTarget{}, fmt.Errorf("compute: get backup target: %w", err)
	}
	return row, nil
}

// ListBackupTargets returns a page of targets within the tenant.
func (s *Service) ListBackupTargets(
	ctx context.Context,
	_ uuid.UUID,
	limit, offset int32,
) ([]database.ComputeBackupTarget, error) {
	return s.repos.ComputeBackupTargets.List(ctx, limit, offset)
}

// CountBackupTargets returns the total number of targets.
func (s *Service) CountBackupTargets(ctx context.Context, _ uuid.UUID) (int64, error) {
	return s.repos.ComputeBackupTargets.Count(ctx)
}

// DeleteBackupTarget soft-deletes the target. Existing backups stay
// readable; the worker stops queueing new backups to it.
func (s *Service) DeleteBackupTarget(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	targetID uuid.UUID,
) error {
	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditBackupTargetDelete,
		ResourceType: ResourceBackupTarget,
		ResourceID:   &targetID,
		Status:       audit.StatusPending,
	})
	if err := s.repos.ComputeBackupTargets.SoftDelete(ctx, targetID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("compute: delete backup target: %w", err)
	}
	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return nil
}

// BuildBackupTargetDriver constructs the BackupTarget driver for the row,
// decrypting the secret envelope via the AES-GCM crypto. Used by the
// worker + the restore path. The returned driver has NOT been pinged.
func (s *Service) BuildBackupTargetDriver(
	ctx context.Context,
	targetID uuid.UUID,
) (BackupTarget, error) {
	if s.crypto == nil {
		return nil, ErrCryptoRequired
	}
	row, err := s.repos.ComputeBackupTargets.Get(ctx, targetID)
	if err != nil {
		if database.IsNoRows(err) {
			return nil, ErrBackupTargetNotFound
		}
		return nil, fmt.Errorf("compute: get backup target: %w", err)
	}
	cfg, err := DecodeConfig(row.ConfigJson)
	if err != nil {
		return nil, err
	}
	plain, err := s.crypto.Open(string(row.EncryptedSecretJson))
	if err != nil {
		return nil, fmt.Errorf("compute: decrypt backup target secret: %w", err)
	}
	secret, err := DecodeSecret([]byte(plain))
	if err != nil {
		return nil, err
	}
	return NewBackupTarget(row.Kind, cfg, secret)
}

// ListBackups returns a page of backups within the tenant.
func (s *Service) ListBackups(
	ctx context.Context,
	_ uuid.UUID,
	limit, offset int32,
) ([]database.ComputeBackup, error) {
	return s.repos.ComputeBackups.List(ctx, limit, offset)
}

// ListBackupsByInstance returns a page of backups for a given instance.
func (s *Service) ListBackupsByInstance(
	ctx context.Context,
	_ uuid.UUID,
	instanceID uuid.UUID,
	limit, offset int32,
) ([]database.ComputeBackup, error) {
	return s.repos.ComputeBackups.ListByInstance(ctx, instanceID, limit, offset)
}

// GetBackup returns the backup row.
func (s *Service) GetBackup(
	ctx context.Context,
	_ uuid.UUID,
	backupID uuid.UUID,
) (database.ComputeBackup, error) {
	row, err := s.repos.ComputeBackups.Get(ctx, backupID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.ComputeBackup{}, ErrBackupNotFound
		}
		return database.ComputeBackup{}, fmt.Errorf("compute: get backup: %w", err)
	}
	return row, nil
}

// CountBackups returns the total number of backups.
func (s *Service) CountBackups(ctx context.Context, _ uuid.UUID) (int64, error) {
	return s.repos.ComputeBackups.Count(ctx)
}

// DeleteBackup orchestrates a backup delete: remove the remote bytes via
// the target driver, then soft-delete the row.
func (s *Service) DeleteBackup(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	backupID uuid.UUID,
) error {
	row, err := s.repos.ComputeBackups.Get(ctx, backupID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrBackupNotFound
		}
		return fmt.Errorf("compute: get backup: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditBackupDelete,
		ResourceType: ResourceBackup,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
	})

	// Best-effort remote delete. A failure here is logged but does not
	// block the row soft-delete — the operator can clean up the remote
	// bytes manually. The audit row records the failure detail.
	if driver, err := s.BuildBackupTargetDriver(ctx, row.TargetID); err == nil {
		if remote := backupRemoteKey(row); remote != "" {
			if err := driver.Delete(ctx, remote); err != nil {
				_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{
					Status:  audit.StatusFailure,
					Details: map[string]any{"remote_delete_error": err.Error()},
				})
				return fmt.Errorf("compute: delete backup remote: %w", err)
			}
		}
	}

	if err := s.repos.ComputeBackups.SoftDelete(ctx, backupID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("compute: delete backup row: %w", err)
	}

	// Best-effort event bus emit.
	s.emitEvent(ctx, eventbus.ComputeBackupDeleted, tenantID, userID, row.ID, map[string]any{
		"snapshot_id": row.SnapshotID,
		"target_id":   row.TargetID,
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return nil
}

// PushBackup is the worker entrypoint: streams the snapshot tarball from
// Incus to the BackupTarget. The backup row is created before the upload
// so a worker retry sees an existing pending row + idempotently skips.
//
// The actor for audit + events is the system; the worker is not acting
// on behalf of a user.
//
//nolint:revive // name stays PushBackup for symmetry with the snapshot Take path.
func (s *Service) PushBackup(
	ctx context.Context,
	tenantID uuid.UUID,
	snapshotID uuid.UUID,
	targetID uuid.UUID,
) (database.ComputeBackup, error) {
	if s.provider == nil {
		return database.ComputeBackup{}, ErrProviderDisabled
	}
	snap, body, err := s.ExportSnapshotBytes(ctx, tenantID, snapshotID)
	if err != nil {
		return database.ComputeBackup{}, err
	}
	driver, err := s.BuildBackupTargetDriver(ctx, targetID)
	if err != nil {
		return database.ComputeBackup{}, err
	}
	// Create the row before the upload so the worker can retry idempotently.
	row, err := s.repos.ComputeBackups.Create(ctx, database.CreateComputeBackupParams{
		SnapshotID: snapshotID,
		InstanceID: snap.InstanceID,
		TargetID:   targetID,
		Status:     database.BackupStatusUploading,
	})
	if err != nil {
		return database.ComputeBackup{}, fmt.Errorf("compute: create backup row: %w", err)
	}
	key := backupKeyFor(snap.ID, row.ID)
	size, checksum, err := driver.Upload(ctx, key, bytes.NewReader(body))
	if err != nil {
		_ = s.repos.ComputeBackups.SetStatus(ctx, row.ID, database.BackupStatusFailed, err.Error())
		return database.ComputeBackup{}, fmt.Errorf("compute: upload backup: %w", err)
	}
	remoteLoc := backupRemoteLocation(driver.Kind(), key)
	if err := s.repos.ComputeBackups.SetResult(ctx, row.ID, size, checksum, remoteLoc); err != nil {
		return database.ComputeBackup{}, fmt.Errorf("compute: set backup result: %w", err)
	}
	row.SizeBytes = size
	row.ChecksumSha256 = checksum
	row.RemoteLocation = remoteLoc
	row.Status = database.BackupStatusCompleted

	s.emitEvent(ctx, eventbus.ComputeBackupCreated, tenantID, uuid.Nil, row.ID, map[string]any{
		"snapshot_id": snapshotID,
		"target_id":   targetID,
		"size_bytes":  size,
		"remote":      remoteLoc,
	})
	return row, nil
}

// DownloadBackupReader opens the remote backup for streaming. The caller
// owns the returned reader and MUST Close it. Used by the restore path.
func (s *Service) DownloadBackupReader(
	ctx context.Context,
	_ uuid.UUID,
	backupID uuid.UUID,
) (database.ComputeBackup, io.ReadCloser, error) {
	row, err := s.repos.ComputeBackups.Get(ctx, backupID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.ComputeBackup{}, nil, ErrBackupNotFound
		}
		return database.ComputeBackup{}, nil, fmt.Errorf("compute: get backup: %w", err)
	}
	driver, err := s.BuildBackupTargetDriver(ctx, row.TargetID)
	if err != nil {
		return database.ComputeBackup{}, nil, err
	}
	r, err := driver.Download(ctx, backupRemoteKey(row))
	if err != nil {
		return database.ComputeBackup{}, nil, fmt.Errorf("compute: download backup: %w", err)
	}
	return row, r, nil
}

// -------------------------------------------------------------------------
// Key + remote-location helpers
// -------------------------------------------------------------------------

// backupKeyFor is the storage key for a backup object. Includes both
// snapshot_id + backup_id so a per-policy retention worker can iterate
// cleanly.
func backupKeyFor(snapshotID, backupID uuid.UUID) string {
	return "snapshots/" + snapshotID.String() + "/backups/" + backupID.String() + ".tar.gz"
}

// backupRemoteKey recovers the storage key from a backup row. The
// remote_location column carries the human-readable URI ("s3://bucket/key");
// the storage key is the path component. For drivers that did not
// populate remote_location (very early rows), fall back to backupKeyFor.
func backupRemoteKey(row database.ComputeBackup) string {
	if row.RemoteLocation == "" {
		return backupKeyFor(row.SnapshotID, row.ID)
	}
	// All drivers store the path after the last "://" or ":" separator;
	// the bucket/host prefix is irrelevant for the storage-key API.
	idx := strings.Index(row.RemoteLocation, "://")
	rest := row.RemoteLocation
	if idx >= 0 {
		rest = row.RemoteLocation[idx+3:]
		// Drop the bucket segment (first path component).
		if slash := strings.Index(rest, "/"); slash >= 0 {
			rest = rest[slash+1:]
		}
	} else if colon := strings.Index(rest, ":"); colon >= 0 {
		// SSH-style "host:path" — drop the host prefix.
		rest = rest[colon+1:]
	}
	rest = strings.TrimLeft(rest, "/")
	if rest == "" {
		return backupKeyFor(row.SnapshotID, row.ID)
	}
	return rest
}

// backupRemoteLocation renders the human-readable URI for a stored key
// given the driver kind. Used by the backup worker to populate the
// remote_location column.
func backupRemoteLocation(kind, key string) string {
	switch kind {
	case BackupTargetKindS3:
		return "s3://" + key
	case BackupTargetKindSSH:
		return "ssh://" + key
	case BackupTargetKindNFS:
		return "file://" + key
	}
	return key
}
