// Package compute: backups_drivers_test.go exercises the BackupTarget
// drivers that can be tested without real external services. The S3 + SSH
// drivers require live endpoints (or fakes the WS-25 scope does not
// include); the local driver is filesystem-only so we cover it here, plus
// the factory + config/secret round-trip.
package compute

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackupTarget_EncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()
	cfg := BackupTargetConfig{
		S3Endpoint:       "https://s3.example.com",
		S3Bucket:         "backups",
		S3Region:         "us-east-1",
		S3Prefix:         "lahijan",
		S3ForcePathStyle: true,
	}
	raw, err := EncodeConfig(cfg)
	require.NoError(t, err)

	got, err := DecodeConfig(raw)
	require.NoError(t, err)
	assert.Equal(t, cfg, got)

	sec := BackupTargetSecret{S3AccessKeyID: "AKIA", S3SecretKey: "shh"}
	secRaw, err := EncodeSecret(sec)
	require.NoError(t, err)
	gotSec, err := DecodeSecret(secRaw)
	require.NoError(t, err)
	assert.Equal(t, sec, gotSec)
}

func TestBackupTarget_FactoryUnknownKind(t *testing.T) {
	t.Parallel()
	_, err := NewBackupTarget("ftp", BackupTargetConfig{}, BackupTargetSecret{})
	require.ErrorIs(t, err, ErrUnknownBackupTargetKind)
}

func TestBackupTarget_FactoryS3(t *testing.T) {
	t.Parallel()
	d, err := NewBackupTarget(
		BackupTargetKindS3,
		BackupTargetConfig{S3Endpoint: "https://s3.example.com", S3Bucket: "b"},
		BackupTargetSecret{S3AccessKeyID: "a", S3SecretKey: "s"},
	)
	require.NoError(t, err)
	assert.Equal(t, BackupTargetKindS3, d.Kind())
}

func TestLocalDriver_UploadDownloadDelete(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	d := NewLocalDriver(BackupTargetConfig{NFSMountPoint: root}, BackupTargetSecret{})

	ctx := context.Background()
	require.NoError(t, d.Ping(ctx))

	body := []byte("hello-snapshot")
	n, checksum, err := d.Upload(ctx, "inst-1/snap-1.tar.gz", bytes.NewReader(body))
	require.NoError(t, err)
	assert.Equal(t, int64(len(body)), n)
	assert.NotEmpty(t, checksum, "checksum must be populated")

	// The file landed at root/inst-1/snap-1.tar.gz.
	_, err = os.Stat(filepath.Join(root, "inst-1", "snap-1.tar.gz"))
	require.NoError(t, err)

	r, err := d.Download(ctx, "inst-1/snap-1.tar.gz")
	require.NoError(t, err)
	got := make([]byte, len(body))
	_, err = r.Read(got)
	require.NoError(t, err)
	assert.Equal(t, body, got)
	require.NoError(t, r.Close(), "close reader before delete so Windows releases the handle")

	// Delete is idempotent.
	require.NoError(t, d.Delete(ctx, "inst-1/snap-1.tar.gz"))
	require.NoError(t, d.Delete(ctx, "inst-1/snap-1.tar.gz"), "delete of missing key is a no-op")
}

func TestLocalDriver_UnsafeKey(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	d := NewLocalDriver(BackupTargetConfig{NFSMountPoint: root}, BackupTargetSecret{})
	ctx := context.Background()

	for _, key := range []string{"../escape", "/absolute", ""} {
		_, _, err := d.Upload(ctx, key, bytes.NewReader([]byte("x")))
		require.Error(t, err, "key %q must be rejected", key)
	}
}

func TestLocalDriver_PingMissingMountPoint(t *testing.T) {
	t.Parallel()
	d := NewLocalDriver(BackupTargetConfig{}, BackupTargetSecret{})
	err := d.Ping(context.Background())
	require.Error(t, err)
}

func TestLocalDriver_Kind(t *testing.T) {
	t.Parallel()
	d := NewLocalDriver(BackupTargetConfig{NFSMountPoint: t.TempDir()}, BackupTargetSecret{})
	assert.Equal(t, BackupTargetKindNFS, d.Kind())
}

func TestSSHDriver_NewDriverValidation(t *testing.T) {
	t.Parallel()
	// Empty private key yields a usable driver (Ping will fail later);
	// an unparseable private key fails at NewSSHDriver.
	d, err := NewSSHDriver(BackupTargetConfig{SSHHost: "h"}, BackupTargetSecret{})
	require.NoError(t, err)
	assert.Equal(t, BackupTargetKindSSH, d.Kind())

	_, err = NewSSHDriver(BackupTargetConfig{SSHHost: "h"},
		BackupTargetSecret{SSHPrivateKey: "not-a-key"})
	require.Error(t, err, "unparseable private key must fail at construction")
}
