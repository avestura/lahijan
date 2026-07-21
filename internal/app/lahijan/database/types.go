// Package database: types.go re-exports the sqlc-generated row types under the
// database package so services never need to import gen directly (ADR-0003: the
// repository layer is the only sanctioned entry point to the DB). These are
// type aliases, not wrappers, so the generated Querier return types flow
// through unchanged.
package database

import "github.com/avestura/lahijan/internal/app/lahijan/database/gen"

// Row type aliases for the sqlc-generated models. Services and tests reference
// these (e.g. database.User) instead of gen.User.
type (
	User                = gen.User
	Tenant              = gen.Tenant
	Session             = gen.Session
	RefreshToken        = gen.RefreshToken
	PersonalAccessToken = gen.PersonalAccessToken
	EmailToken          = gen.EmailToken
	Membership          = gen.Membership
	Role                = gen.Role
	Permission          = gen.Permission
	AuditLog            = gen.AuditLog
	AuditLogOutcome     = gen.AuditLogOutcome
	UserOauthIdentity   = gen.UserOauthIdentity
	UserSamlIdentity    = gen.UserSamlIdentity
	Plugin              = gen.Plugin
	PluginPermission    = gen.PluginPermission

	// ComputeInstance / ComputeImage / ComputeProfile / ComputeNetwork /
	// ComputeStorageVolume are the row aliases (WS-14). Services reference
	// these (e.g. database.ComputeInstance) instead of gen.ComputeInstance.
	// WS-25 adds ComputeSnapshot / ComputeSnapshotPolicy /
	// ComputeBackupTarget / ComputeBackup aliases for the snapshot +
	// off-host-backup surface.
	ComputeInstance       = gen.ComputeInstance
	ComputeImage          = gen.ComputeImage
	ComputeProfile        = gen.ComputeProfile
	ComputeNetwork        = gen.ComputeNetwork
	ComputeStorageVolume  = gen.ComputeStorageVolume
	ComputeSnapshot       = gen.ComputeSnapshot
	ComputeSnapshotPolicy = gen.ComputeSnapshotPolicy
	ComputeBackupTarget   = gen.ComputeBackupTarget
	ComputeBackup         = gen.ComputeBackup

	// DNSZone / DNSRecord / DNSDomain are the row aliases (WS-12, WS-15,
	// WS-28). Services reference these (e.g. database.DNSZone) instead
	// of gen.DnsZone. The sqlc-generated names use the snake_case table
	// name (DnsZone / DnsRecord / DnsDomain); the database package
	// re-exports them under the idiomatic-Go DNS prefix without
	// changing the underlying type (these are type aliases, not
	// wrappers).
	DNSZone   = gen.DnsZone
	DNSRecord = gen.DnsRecord
	DNSDomain = gen.DnsDomain

	// StorageBucket / StorageCredential are the row aliases (WS-16).
	// Services reference these (e.g. database.StorageBucket) instead of
	// gen.StorageBucket. The sqlc-generated names already match the
	// idiomatic-Go form so the aliases are passthrough; they exist so
	// services never need to import the gen package directly.
	// WS-29 adds StorageLifecycleRule for the per-bucket lifecycle
	// surface.
	StorageBucket       = gen.StorageBucket
	StorageCredential   = gen.StorageCredential
	StorageLifecycleRule = gen.StorageLifecycleRule

	// Price is the billing price catalog row alias (WS-17).
	// LedgerEntry / UsageEvent / Receipt / UserBalance mirror the
	// other billing tables so services can reference them without
	// importing gen.
	Price       = gen.Price
	LedgerEntry = gen.LedgerEntry
	UsageEvent  = gen.UsageEvent
	Receipt     = gen.Receipt
	UserBalance = gen.UserBalance
)
