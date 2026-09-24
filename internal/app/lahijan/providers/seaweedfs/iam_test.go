// Package seaweedfs_test: iam_test.go covers the per-user S3 credential
// mint / revoke / rotate cycle. Each test exercises one of the DoD
// bullets: "mint credentials scoped to one bucket", "revoke credentials
// → user can no longer access", "rotate".
package seaweedfs_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIAM_Mint_ScopedToOneBucket(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "uploads")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket, TenantID: validTenantUUID})
	require.NoError(t, err)

	cred, err := p.MintCredentials(ctx, seaweedfs.MintCredentialsParams{
		TenantID: validTenantUUID,
		Buckets:  []string{bucket},
		Actions:  []seaweedfs.IAMAction{seaweedfs.IAMActionRead, seaweedfs.IAMActionWrite},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, cred.AccessKey, "AccessKey must be populated")
	assert.NotEmpty(t, cred.SecretKey, "SecretKey must be populated at mint time")
	assert.True(t, cred.Enabled)
	assert.Contains(t, cred.Buckets, bucket)

	// The fake retains the scope. Subsequent reads return it.
	scope := srv.IdentityBuckets(cred.AccessKey)
	assert.Contains(t, scope, bucket)
	actions := srv.IdentityActions(cred.AccessKey)
	// expandActions produces Read:<bucket> + Write:<bucket>.
	assert.Contains(t, actions, "Read:"+bucket)
	assert.Contains(t, actions, "Write:"+bucket)
}

func TestIAM_Mint_Validation(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Run("no-buckets", func(t *testing.T) {
		t.Parallel()
		_, err := p.MintCredentials(ctx, seaweedfs.MintCredentialsParams{
			Actions: []seaweedfs.IAMAction{seaweedfs.IAMActionRead},
		})
		require.Error(t, err)
	})
	t.Run("no-actions", func(t *testing.T) {
		t.Parallel()
		_, err := p.MintCredentials(ctx, seaweedfs.MintCredentialsParams{
			Buckets: []string{mustBucketName(t, "x")},
		})
		require.Error(t, err)
	})
	t.Run("bad-action", func(t *testing.T) {
		t.Parallel()
		_, err := p.MintCredentials(ctx, seaweedfs.MintCredentialsParams{
			Buckets: []string{mustBucketName(t, "y")},
			Actions: []seaweedfs.IAMAction{"FlyToTheMoon"},
		})
		require.Error(t, err)
	})
	t.Run("bad-bucket", func(t *testing.T) {
		t.Parallel()
		_, err := p.MintCredentials(ctx, seaweedfs.MintCredentialsParams{
			Buckets: []string{"UPPERCASE"},
			Actions: []seaweedfs.IAMAction{seaweedfs.IAMActionRead},
		})
		require.Error(t, err)
	})
}

func TestIAM_Get_RedactsSecretKey(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "x")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	cred, err := p.MintCredentials(ctx, seaweedfs.MintCredentialsParams{
		Buckets: []string{bucket},
		Actions: []seaweedfs.IAMAction{seaweedfs.IAMActionRead},
	})
	require.NoError(t, err)

	// Read back: secret key MUST be empty.
	got, err := p.GetCredential(ctx, cred.AccessKey)
	require.NoError(t, err)
	assert.Equal(t, cred.AccessKey, got.AccessKey)
	assert.Empty(t, got.SecretKey, "secret key must NOT be returned on read paths")
	assert.True(t, got.Enabled)
}

func TestIAM_Revoke_BlocksFurtherAccess(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "x")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)
	cred, err := p.MintCredentials(ctx, seaweedfs.MintCredentialsParams{
		Buckets: []string{bucket},
		Actions: []seaweedfs.IAMAction{seaweedfs.IAMActionRead},
	})
	require.NoError(t, err)
	require.True(t, srv.HasIdentity(cred.AccessKey))

	// Revoke → identity record is removed; the access key stops signing.
	require.NoError(t, p.RevokeCredentials(ctx, cred.AccessKey))
	assert.False(t, srv.HasIdentity(cred.AccessKey))

	// GetCredential returns ErrNotFound on a revoked credential.
	_, err = p.GetCredential(ctx, cred.AccessKey)
	require.Error(t, err)
}

func TestIAM_Revoke_Idempotent(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Revoking an unknown access key returns nil (idempotent).
	require.NoError(t, p.RevokeCredentials(ctx, "lah_unknown_access_key"))
}

func TestIAM_Rotate_ChangesAccessKey(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "x")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)
	cred, err := p.MintCredentials(ctx, seaweedfs.MintCredentialsParams{
		Buckets: []string{bucket},
		Actions: []seaweedfs.IAMAction{seaweedfs.IAMActionWrite},
	})
	require.NoError(t, err)
	oldAccessKey := cred.AccessKey

	// Rotate → old access key is gone; new one is returned with the
	// same bucket scope.
	rotated, err := p.RotateCredentials(ctx, oldAccessKey, seaweedfs.MintCredentialsParams{
		Buckets: []string{bucket},
		Actions: []seaweedfs.IAMAction{seaweedfs.IAMActionWrite},
	})
	require.NoError(t, err)
	assert.NotEqual(t, oldAccessKey, rotated.AccessKey, "Rotate MUST mint a new access key")
	assert.NotEmpty(t, rotated.SecretKey)
	assert.Contains(t, rotated.Buckets, bucket)
	assert.False(t, srv.HasIdentity(oldAccessKey), "old access key MUST be revoked")
	assert.True(t, srv.HasIdentity(rotated.AccessKey), "new access key MUST be live")
}

func TestIAM_Rotate_NotFound(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := p.RotateCredentials(ctx, "lah_unknown", seaweedfs.MintCredentialsParams{
		Buckets: []string{mustBucketName(t, "y")},
		Actions: []seaweedfs.IAMAction{seaweedfs.IAMActionRead},
	})
	require.Error(t, err)
}

func TestIAM_ListCredentials(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "x")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	const n = 3
	for i := 0; i < n; i++ {
		_, err := p.MintCredentials(ctx, seaweedfs.MintCredentialsParams{
			Buckets: []string{bucket},
			Actions: []seaweedfs.IAMAction{seaweedfs.IAMActionRead},
		})
		require.NoError(t, err)
	}
	creds, err := p.ListCredentials(ctx)
	require.NoError(t, err)
	assert.Len(t, creds, n)
	for _, c := range creds {
		assert.Empty(t, c.SecretKey, "ListCredentials must NOT return secret keys")
	}
}

func TestIAM_Mint_DistinctAccessKeys(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "x")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	const n = 10
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		cred, err := p.MintCredentials(ctx, seaweedfs.MintCredentialsParams{
			Buckets: []string{bucket},
			Actions: []seaweedfs.IAMAction{seaweedfs.IAMActionRead},
		})
		require.NoError(t, err)
		require.False(t, seen[cred.AccessKey], "duplicate access key minted: %q", cred.AccessKey)
		seen[cred.AccessKey] = true
		require.False(t, seen[cred.SecretKey], "duplicate secret key minted")
		seen[cred.SecretKey] = true
	}
}

// TestIAM_PublishesSeaweedIAMDocument guards the real-daemon contract:
// `weed s3` only hot-reloads /etc/iam/identity.json, and that document
// replaces its whole identity set — so every mint/revoke must re-publish
// it with the admin identity plus each enabled minted identity.
func TestIAM_PublishesSeaweedIAMDocument(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "iamdoc")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket, TenantID: validTenantUUID})
	require.NoError(t, err)
	cred, err := p.MintCredentials(ctx, seaweedfs.MintCredentialsParams{
		TenantID: validTenantUUID,
		Buckets:  []string{bucket},
		Actions:  []seaweedfs.IAMAction{seaweedfs.IAMActionRead},
	})
	require.NoError(t, err)

	type doc struct {
		Identities []struct {
			Credentials []struct {
				AccessKey string `json:"accessKey"`
			} `json:"credentials"`
			Actions []string `json:"actions"`
		} `json:"identities"`
	}
	load := func() map[string][]string {
		raw, err := srv.GetMetadata(ctx, "/etc/iam/identity.json")
		require.NoError(t, err)
		var d doc
		require.NoError(t, json.Unmarshal(raw, &d))
		out := map[string][]string{}
		for _, id := range d.Identities {
			out[id.Credentials[0].AccessKey] = id.Actions
		}
		return out
	}

	ids := load()
	assert.Contains(t, ids, "lahijan-dev-admin-key", "admin identity must survive every publish")
	assert.Equal(t, []string{"Read:" + bucket}, ids[cred.AccessKey])

	require.NoError(t, p.RevokeCredentials(ctx, cred.AccessKey))
	ids = load()
	assert.Contains(t, ids, "lahijan-dev-admin-key")
	assert.NotContains(t, ids, cred.AccessKey, "revoked identity must drop out of the IAM document")
}
