// Package hostfuncs: services.go defines the narrow service interfaces the
// compute, dns, and storage host modules depend on (WS-10f). Each interface
// mirrors the subset of the real service's method surface that host
// functions call. The concrete *compute.Service / *dns.Service /
// *storage.Service structs satisfy these interfaces structurally; the
// program layer injects them via hostfuncs.Deps.
package hostfuncs

import (
	"context"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/compute"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/dns"
	"github.com/avestura/lahijan/internal/app/lahijan/storage"
)

// ComputeOps is the compute service surface available to host functions.
// Every method enforces tenant scoping + billing + audit internally.
type ComputeOps interface {
	CreateInstance(ctx context.Context, tenantID, userID uuid.UUID, params compute.InstanceCreateParams) (database.ComputeInstance, error)
	GetInstance(ctx context.Context, tenantID, instanceID uuid.UUID) (database.ComputeInstance, error)
	ListInstances(ctx context.Context, tenantID uuid.UUID, limit, offset int32) ([]database.ComputeInstance, error)
	SetInstanceState(ctx context.Context, tenantID, userID uuid.UUID, instanceID uuid.UUID, action compute.InstanceLifecycleAction, force bool, timeoutSecs int) (database.ComputeInstance, error)
	DeleteInstance(ctx context.Context, tenantID, userID uuid.UUID, instanceID uuid.UUID, force bool) error
}

// DNSOps is the DNS service surface available to host functions.
type DNSOps interface {
	CreateZone(ctx context.Context, tenantID, userID uuid.UUID, params dns.ZoneCreateParams) (database.DNSZone, error)
	GetZone(ctx context.Context, tenantID, zoneID uuid.UUID) (database.DNSZone, error)
	ListZones(ctx context.Context, tenantID uuid.UUID, limit, offset int32) ([]database.DNSZone, error)
	DeleteZone(ctx context.Context, tenantID, userID uuid.UUID, zoneID uuid.UUID) error

	CreateRecord(ctx context.Context, tenantID, userID uuid.UUID, zoneID uuid.UUID, params dns.RecordCreateParams) (database.DNSRecord, error)
	ListRecords(ctx context.Context, tenantID uuid.UUID, zoneID uuid.UUID, limit, offset int32) ([]database.DNSRecord, error)
	DeleteRecord(ctx context.Context, tenantID, userID uuid.UUID, zoneID, recordID uuid.UUID) error
}

// StorageOps is the storage service surface available to host functions.
type StorageOps interface {
	CreateBucket(ctx context.Context, tenantID, userID uuid.UUID, params storage.BucketCreateParams) (database.StorageBucket, error)
	GetBucket(ctx context.Context, tenantID uuid.UUID, bucketID uuid.UUID) (database.StorageBucket, error)
	ListBuckets(ctx context.Context, tenantID uuid.UUID, limit, offset int32) ([]database.StorageBucket, error)
	DeleteBucket(ctx context.Context, tenantID, userID uuid.UUID, bucketID uuid.UUID) error
}
