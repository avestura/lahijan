// Package seaweedfs_test: events_test.go covers the change-event synthesis
// path. SeaweedFS does not push events; the driver emits one on every
// mutating call so the WASM bus sees a uniform stream. The test asserts
// the topic names + the basic payload shape for each mutation.
package seaweedfs_test

import (
	"context"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvents_BucketCreatedEmits(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	bus := newRecordingBus()
	p := connectProviderWithBus(t, srv, bus)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "evt-create")
	created, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket, TenantID: validTenantUUID})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(bus.Snapshot()) >= 1 },
		2*time.Second, 25*time.Millisecond)
	events := bus.Snapshot()
	require.NotEmpty(t, events)
	last := events[len(events)-1]
	assert.Equal(t, "storage.bucket.created", last.Topic)
	require.NotNil(t, last.ResourceID)
	assert.Equal(t, created.Name, *last.ResourceID)
	assert.Equal(t, "system", last.ActorType)
	require.NotNil(t, last.TenantID, "TenantID MUST be carried for tenant-scoped creates")
	assert.Equal(t, validTenantUUID, *last.TenantID)
}

func TestEvents_BucketDeletedEmits(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	bus := newRecordingBus()
	p := connectProviderWithBus(t, srv, bus)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "evt-del")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket, TenantID: validTenantUUID})
	require.NoError(t, err)
	require.NoError(t, p.DeleteBucket(ctx, bucket))

	require.Eventually(t, func() bool { return len(bus.Topics()) >= 2 },
		2*time.Second, 25*time.Millisecond)
	topics := bus.Topics()
	assert.Equal(t, "storage.bucket.created", topics[0])
	assert.Equal(t, "storage.bucket.deleted", topics[1])
}

func TestEvents_CredentialMintedEmits(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	bus := newRecordingBus()
	p := connectProviderWithBus(t, srv, bus)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "x")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket, TenantID: validTenantUUID})
	require.NoError(t, err)

	cred, err := p.MintCredentials(ctx, seaweedfs.MintCredentialsParams{
		TenantID: validTenantUUID,
		Buckets:  []string{bucket},
		Actions:  []seaweedfs.IAMAction{seaweedfs.IAMActionRead},
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		for _, topic := range bus.Topics() {
			if topic == "storage.credential.minted" {
				return true
			}
		}
		return false
	}, 2*time.Second, 25*time.Millisecond)

	var minted *seaweedfs.BusEvent
	for _, e := range bus.Snapshot() {
		if e.Topic == "storage.credential.minted" {
			minted = &e
			break
		}
	}
	require.NotNil(t, minted)
	require.NotNil(t, minted.ResourceID)
	assert.Equal(t, cred.AccessKey, *minted.ResourceID)
}

func TestEvents_CredentialRevokedEmits(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	bus := newRecordingBus()
	p := connectProviderWithBus(t, srv, bus)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "x")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket, TenantID: validTenantUUID})
	require.NoError(t, err)
	cred, err := p.MintCredentials(ctx, seaweedfs.MintCredentialsParams{
		TenantID: validTenantUUID,
		Buckets:  []string{bucket},
		Actions:  []seaweedfs.IAMAction{seaweedfs.IAMActionRead},
	})
	require.NoError(t, err)
	require.NoError(t, p.RevokeCredentials(ctx, cred.AccessKey))

	require.Eventually(t, func() bool {
		for _, topic := range bus.Topics() {
			if topic == "storage.credential.revoked" {
				return true
			}
		}
		return false
	}, 2*time.Second, 25*time.Millisecond)
}

func TestEvents_QuotaSetEmits(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	bus := newRecordingBus()
	p := connectProviderWithBus(t, srv, bus)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "x")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket, TenantID: validTenantUUID})
	require.NoError(t, err)

	require.NoError(t, p.SetBucketQuota(ctx, bucket, seaweedfs.QuotaSpec{SizeMiB: 5}))
	require.Eventually(t, func() bool {
		for _, topic := range bus.Topics() {
			if topic == "storage.bucket.quota.set" {
				return true
			}
		}
		return false
	}, 2*time.Second, 25*time.Millisecond)
}

func TestEvents_NoBus_IsSilentNoOp(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	// Provider built without a bus — the emit path must not panic and
	// must not block the request.
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{
		Bucket: mustBucketName(t, "no-bus"), TenantID: validTenantUUID,
	})
	require.NoError(t, err, "CreateBucket must succeed with no bus attached")
}
