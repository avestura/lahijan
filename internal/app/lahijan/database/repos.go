// Package database: repos.go defines the repository layer that every service
// uses to reach Postgres. Each repository embeds the sqlc-generated *gen.Queries
// and adds typed methods; tenant-scoped repositories additionally bake the
// tenant id from the request context into every query (ADR-0002). Services
// never construct a *gen.Queries directly.
package database

import "github.com/avestura/lahijan/internal/app/lahijan/database/gen"

// DBTX is re-exported so callers (and tests) can build repos from a pool or a
// transaction without importing the gen package.
type DBTX = gen.DBTX

// Repos is the aggregate of all repositories. Wire it into services once at
// bootstrap (program.Start) and pass the same instance to every handler.
type Repos struct {
	Tenants               *TenantsRepository
	Users                 *UsersRepository
	RBAC                  *RBACRepository
	Memberships           *MembershipsRepository
	AuditLog              *AuditLogRepository
	Tokens                *TokensRepository
	Sessions              *SessionsRepository
	EmailTokens           *EmailTokensRepository
	OAuthIdentities       *OAuthIdentitiesRepository
	SamlIdentities        *SamlIdentitiesRepository
	TOTPSecrets           *TOTPSecretsRepository
	WebauthnCreds         *WebauthnCredentialsRepository
	RecoveryCodes         *RecoveryCodesRepository
	MFAPending            *MFAPendingSessionsRepository
	Plugins               *PluginsRepository
	PluginKV              *PluginKVRepository
	PluginConfig          *PluginConfigRepository
	PluginSubscriptions   *PluginEventSubscriptionsRepository
	PluginHTTPHandlers    *PluginHTTPHandlersRepository
	DNSZones              *DNSZonesRepository
	DNSRecords            *DNSRecordsRepository
	DNSDomains            *DNSDomainsRepository
	ComputeInstances      *ComputeInstancesRepository
	ComputeImages         *ComputeImagesRepository
	ComputeProfiles       *ComputeProfilesRepository
	ComputeNetworks       *ComputeNetworksRepository
	ComputeStorageVolumes *ComputeStorageVolumesRepository
	// WS-25: scheduled snapshots + off-host backups. Four sub-repositories:
	// the per-snapshot rows (ComputeSnapshots), the per-tenant off-host
	// destinations (ComputeBackupTargets), the per-snapshot exported
	// backups (ComputeBackups), and the per-instance + per-tenant schedule
	// policies (ComputeSnapshotPolicies).
	ComputeSnapshots        *ComputeSnapshotsRepository
	ComputeBackupTargets    *ComputeBackupTargetsRepository
	ComputeBackups          *ComputeBackupsRepository
	ComputeSnapshotPolicies *ComputeSnapshotPoliciesRepository
	StorageBuckets          *StorageBucketsRepository
	StorageCredentials      *StorageCredentialsRepository
	// WS-29: per-bucket S3 lifecycle rules. Source-of-truth on the
	// Lahijan side; pushed to SeaweedFS via the SDK + evaluated by the
	// storage.lifecycle.evaluate River worker.
	StorageLifecycleRules *StorageLifecycleRulesRepository

	// WS-30: operator-owned IP pools + per-tenant floating IPs
	// (ADR-0037). IPPools + IPPoolRanges are GLOBAL (operator-owned);
	// FloatingIPs is tenant-scoped. Postgres is the source of truth; the
	// Incus network forward is best-effort.
	IPPools      *IPPoolsRepository
	IPPoolRanges *IPPoolRangesRepository
	FloatingIPs  *FloatingIPsRepository

	// WS-17: billing & metering. Five sub-repositories: the admin-managed
	// price catalog (BillingPrices), the append-only per-user ledger
	// (BillingLedger), the per-user balance cache (BillingBalances), the
	// raw metering stream (BillingUsage), and the per-user-per-period
	// receipts (BillingReceipts).
	BillingPrices   *BillingPricesRepository
	BillingLedger   *BillingLedgerRepository
	BillingBalances *BillingBalancesRepository
	BillingUsage    *BillingUsageRepository
	BillingReceipts *BillingReceiptsRepository

	// WS-27: payment gateway (Stripe). Five sub-repositories: the
	// admin-managed subscription plan catalog (BillingPlans), the
	// per-user Stripe PaymentMethod cache (BillingPaymentMethods),
	// the per-user recurring subscriptions (BillingSubscriptions),
	// the admin-issued promo codes (BillingPromoCodes), and the
	// idempotent Stripe webhook ingestion log (BillingWebhookEvents).
	BillingPlans          *BillingPlansRepository
	BillingPaymentMethods *BillingPaymentMethodsRepository
	BillingSubscriptions  *BillingSubscriptionsRepository
	BillingPromoCodes     *BillingPromoCodesRepository
	BillingWebhookEvents  *BillingWebhookEventsRepository

	// WS-31: AI agent chat. One repository covering the five agent_chat
	// tables (conversations, messages, tool_calls, provider_configs,
	// policy); they share the (tenant_id, user_id) scoping seam. See
	// agent_repo.go.
	Agent *AgentRepository
}

// NewRepos builds the aggregate repository from a pool or transaction. The
// same DBTX (typically the *pgxpool.Pool) is shared by every sub-repo.
func NewRepos(db DBTX) *Repos {
	q := gen.New(db)
	return &Repos{
		Tenants:                 NewTenantsRepository(q),
		Users:                   NewUsersRepository(q),
		RBAC:                    NewRBACRepository(q),
		Memberships:             NewMembershipsRepository(q),
		AuditLog:                NewAuditLogRepository(q),
		Tokens:                  NewTokensRepository(q),
		Sessions:                NewSessionsRepository(q),
		EmailTokens:             NewEmailTokensRepository(q),
		OAuthIdentities:         NewOAuthIdentitiesRepository(q),
		SamlIdentities:          NewSamlIdentitiesRepository(q),
		TOTPSecrets:             NewTOTPSecretsRepository(q),
		WebauthnCreds:           NewWebauthnCredentialsRepository(q),
		RecoveryCodes:           NewRecoveryCodesRepository(q),
		MFAPending:              NewMFAPendingSessionsRepository(q),
		Plugins:                 NewPluginsRepository(q),
		PluginKV:                NewPluginKVRepository(q),
		PluginConfig:            NewPluginConfigRepository(q),
		PluginSubscriptions:     NewPluginEventSubscriptionsRepository(q),
		PluginHTTPHandlers:      NewPluginHTTPHandlersRepository(q),
		DNSZones:                NewDNSZonesRepository(q),
		DNSRecords:              NewDNSRecordsRepository(q),
		DNSDomains:              NewDNSDomainsRepository(q),
		ComputeInstances:        NewComputeInstancesRepository(q),
		ComputeImages:           NewComputeImagesRepository(q),
		ComputeProfiles:         NewComputeProfilesRepository(q),
		ComputeNetworks:         NewComputeNetworksRepository(q),
		ComputeStorageVolumes:   NewComputeStorageVolumesRepository(q),
		ComputeSnapshots:        NewComputeSnapshotsRepository(q),
		ComputeBackupTargets:    NewComputeBackupTargetsRepository(q),
		ComputeBackups:          NewComputeBackupsRepository(q),
		ComputeSnapshotPolicies: NewComputeSnapshotPoliciesRepository(q),
		StorageBuckets:          NewStorageBucketsRepository(q),
		StorageCredentials:      NewStorageCredentialsRepository(q),
		StorageLifecycleRules:   NewStorageLifecycleRulesRepository(q),
		IPPools:                 NewIPPoolsRepository(q),
		IPPoolRanges:            NewIPPoolRangesRepository(q),
		FloatingIPs:             NewFloatingIPsRepository(q),
		BillingPrices:           NewBillingPricesRepository(q),
		BillingLedger:           NewBillingLedgerRepository(q),
		BillingBalances:         NewBillingBalancesRepository(q),
		BillingUsage:            NewBillingUsageRepository(q),
		BillingReceipts:         NewBillingReceiptsRepository(q),
		BillingPlans:            NewBillingPlansRepository(q),
		BillingPaymentMethods:   NewBillingPaymentMethodsRepository(q),
		BillingSubscriptions:    NewBillingSubscriptionsRepository(q),
		BillingPromoCodes:       NewBillingPromoCodesRepository(q),
		BillingWebhookEvents:    NewBillingWebhookEventsRepository(q),
		Agent:                   NewAgentRepository(q),
	}
}
