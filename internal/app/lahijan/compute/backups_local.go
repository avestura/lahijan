// Package compute: backups_local.go is the "nfs" BackupTarget driver. The
// operator pre-mounts the NFS share (or any writable directory) and points
// the target at it; this driver does plain filesystem IO. The kind slug
// is "nfs" in the API so the UI shows the right config shape, but the
// runtime is filesystem-only — true NFS mounting is operator infra, not
// application code.
package compute

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalDriver implements BackupTarget against a locally-mounted directory.
// The driver is safe for concurrent use; filesystem ops are atomic for
// distinct keys and Create + Rename gives us crash-safe uploads.
type LocalDriver struct {
	rootPath string
}

// NewLocalDriver builds a LocalDriver from the per-kind config + decrypted
// secret. The driver has not been pinged; the caller SHOULD call Ping
// before the first upload. The secret's NFSPassword is unused (the
// operator is responsible for the authenticated mount).
func NewLocalDriver(cfg BackupTargetConfig, _ BackupTargetSecret) *LocalDriver {
	root := filepath.Join(cfg.NFSMountPoint, cfg.NFSSubPath)
	if cfg.NFSSubPath == "" {
		root = cfg.NFSMountPoint
	}
	return &LocalDriver{rootPath: root}
}

// Kind returns the driver's kind slug.
func (d *LocalDriver) Kind() string { return BackupTargetKindNFS }

// Ping verifies the directory exists + is writable. Creates the directory
// tree on first use so a freshly-configured target just works.
func (d *LocalDriver) Ping(_ context.Context) error {
	if d.rootPath == "" {
		return errors.New("compute: nfs backup target: mount_point is required")
	}
	if err := os.MkdirAll(d.rootPath, 0o750); err != nil {
		return fmt.Errorf("%w: nfs mkdir: %w", ErrBackupTargetUnreachable, err)
	}
	// Best-effuff write+delete to confirm the directory is writable.
	probe := filepath.Join(d.rootPath, ".lahijan-probe")
	if err := os.WriteFile(probe, []byte("ping"), 0o600); err != nil {
		return fmt.Errorf("%w: nfs write probe: %w", ErrBackupTargetUnreachable, err)
	}
	_ = os.Remove(probe)
	return nil
}

// Upload writes body to a temp file then renames it into place. The
// SHA-256 is computed in-memory; the size is the bytes read.
func (d *LocalDriver) Upload(_ context.Context, key string, body io.Reader) (int64, string, error) {
	cleaned, err := safeJoin(d.rootPath, key)
	if err != nil {
		return 0, "", err
	}
	if mkErr := os.MkdirAll(filepath.Dir(cleaned), 0o750); mkErr != nil {
		return 0, "", fmt.Errorf("compute: nfs mkdir key: %w", mkErr)
	}
	tmp := cleaned + ".lahijan-tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, "", fmt.Errorf("compute: nfs create tmp: %w", err)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), body)
	closeErr := f.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return 0, "", fmt.Errorf("compute: nfs write: %w", err)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return 0, "", fmt.Errorf("compute: nfs close: %w", closeErr)
	}
	if err := os.Rename(tmp, cleaned); err != nil {
		_ = os.Remove(tmp)
		return 0, "", fmt.Errorf("compute: nfs rename: %w", err)
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

// Delete removes the file at key. Idempotent: a missing file is a no-op.
func (d *LocalDriver) Delete(_ context.Context, key string) error {
	cleaned, err := safeJoin(d.rootPath, key)
	if err != nil {
		return err
	}
	if err := os.Remove(cleaned); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("compute: nfs delete: %w", err)
	}
	return nil
}

// Download opens the file at key for reading. The caller owns the
// returned reader and MUST Close it.
func (d *LocalDriver) Download(_ context.Context, key string) (io.ReadCloser, error) {
	cleaned, err := safeJoin(d.rootPath, key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(cleaned)
	if err != nil {
		return nil, fmt.Errorf("compute: nfs open: %w", err)
	}
	return f, nil
}

// safeJoin joins root + key and refuses path-escape attempts (key starting
// with "/" or containing ".." past the root). Returns the cleaned path or
// an error. We do NOT use filepath.Join directly because it does not
// reject absolute keys.
func safeJoin(root, key string) (string, error) {
	if key == "" {
		return "", errors.New("compute: nfs backup target: empty key")
	}
	// Reject the obvious path-traversal shapes BEFORE filepath.Clean
	// normalises them away (Windows strips a leading "/").
	if strings.HasPrefix(key, "/") || strings.HasPrefix(key, "\\") {
		return "", fmt.Errorf("compute: nfs backup target: unsafe key %q", key)
	}
	cleaned := filepath.Clean(key)
	if strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("compute: nfs backup target: unsafe key %q", key)
	}
	return filepath.Join(root, cleaned), nil
}
