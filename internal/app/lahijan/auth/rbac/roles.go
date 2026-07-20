// Package rbac: roles.go declares the default role bundles seeded at bootstrap.
//
// The four seeded tenant roles follow the WS-08 spec and mirror common SaaS
// patterns: owner (full), admin (manage members + resources), member (create
// and manage own resources), viewer (read-only). New tenants are expected to
// start with this set; admins can later create custom roles via the
// rbac.role.* endpoints.
package rbac

// RoleDefinition is the registry shape for a default role: its machine slug,
// its display name, and the permission slugs it bundles. The slug matches an
// i18n key "rbac.role_<slug-with-dots-as-underscores>" for the localised name.
type RoleDefinition struct {
	Slug        string
	Name        string
	Description string
	Permissions []string
	// IsSystem marks roles that cannot be deleted by tenant admins. The four
	// defaults below are system roles. Custom roles created via API are not.
	IsSystem bool
}

// Default role slugs.
const (
	RoleTenantOwner  = "tenant.owner"
	RoleTenantAdmin  = "tenant.admin"
	RoleTenantMember = "tenant.member"
	RoleTenantViewer = "tenant.viewer"

	// RolePlatformAdmin is a global superuser role. Not tenant-scoped; the
	// seeder still creates it (with IsSystem=true) so it can be granted to
	// the bootstrap operator account. Callers must verify the user holds this
	// role globally (across tenants) — the policy evaluator includes it in
	// every HasPermission check as an override.
	RolePlatformAdmin = "platform.admin"
)

// defaultRoles is the registry of seeded roles. The order matters only for
// test output; the seeder is idempotent and order-independent.
var defaultRoles = []RoleDefinition{
	{
		Slug:        RolePlatformAdmin,
		Name:        "Platform Administrator",
		Description: "Global superuser. Bypasses all permission checks.",
		// platform.admin bypasses every check (Evaluator short-circuits on it);
		// we still seed ALL permissions on it so direct queries (e.g. "list
		// permissions for role") return the full catalog.
		Permissions: allPermissionSlugs(),
		IsSystem:    true,
	},
	{
		Slug:        RoleTenantOwner,
		Name:        "Tenant Owner",
		Description: "Full access within the tenant, including deletion and ownership transfer.",
		Permissions: allPermissionSlugsExceptGlobal(),
		IsSystem:    true,
	},
	{
		Slug:        RoleTenantAdmin,
		Name:        "Tenant Administrator",
		Description: "Manage resources, members, and audit within the tenant. Cannot delete the tenant or transfer ownership.",
		Permissions: []string{
			// tenant (no delete, no transfer)
			PermTenantRead,
			PermTenantUpdate,
			PermTenantMemberList,
			PermTenantMemberInvite,
			PermTenantMemberRemove,
			PermTenantMemberRoleUpdate,
			// audit
			PermAuditRead,
			PermAuditExport,
			// rbac
			PermRBACRoleCreate,
			PermRBACRoleUpdate,
			PermRBACRoleDelete,
			PermRBACRoleList,
			// auth
			PermAuthPatManage,
			PermAuthSessionCreate,
			PermAuthSessionRevoke,
			// compute (full)
			PermComputeInstanceCreate,
			PermComputeInstanceRead,
			PermComputeInstanceUpdate,
			PermComputeInstanceStart,
			PermComputeInstanceStop,
			PermComputeInstanceRestart,
			PermComputeInstanceDelete,
			PermComputeInstanceConsoleVNC,
			PermComputeProfileRead,
			PermComputeProfileApply,
			PermComputeImageRead,
			PermComputeNetworkRead,
			PermComputeNetworkCreate,
			PermComputeStoragePoolRead,
			// compute snapshots / backups / schedules (WS-25, admin = full)
			PermComputeSnapshotCreate,
			PermComputeSnapshotRead,
			PermComputeSnapshotDelete,
			PermComputeBackupTargetCreate,
			PermComputeBackupTargetRead,
			PermComputeBackupTargetUpdate,
			PermComputeBackupTargetDelete,
			PermComputeBackupRead,
			PermComputeBackupDelete,
			PermComputeBackupRestore,
			PermComputeSnapshotPolicyCreate,
			PermComputeSnapshotPolicyRead,
			PermComputeSnapshotPolicyUpdate,
			PermComputeSnapshotPolicyDelete,
			// compute cluster (WS-26, admin = full: list members,
			// migrate own instances, evacuate/restore members).
			PermComputeClusterMemberList,
			PermComputeClusterMemberEvacuate,
			PermComputeInstanceMigrate,
			// dns (full)
			PermDNSZoneCreate,
			PermDNSZoneRead,
			PermDNSZoneUpdate,
			PermDNSZoneDelete,
			PermDNSRecordCreate,
			PermDNSRecordRead,
			PermDNSRecordUpdate,
			PermDNSRecordDelete,
			// s3 (full)
			PermS3BucketCreate,
			PermS3BucketRead,
			PermS3BucketUpdate,
			PermS3BucketDelete,
			PermS3ObjectRead,
			PermS3ObjectDelete,
			PermS3CredentialsCreate,
			PermS3CredentialsRevoke,
		// billing
		PermBillingBalanceRead,
		PermBillingBalanceAdjust,
		PermBillingLedgerRead,
		PermBillingReceiptRead,
		PermBillingReceiptCreate,
		PermBillingPriceCatalogRead,
		PermBillingPriceCatalogUpdate,
		// billing payments + subscriptions (WS-27, admin = full)
		PermBillingPaymentMethodManage,
		PermBillingPaymentIntentCreate,
		PermBillingSubscriptionManage,
		PermBillingPromoCodeRedeem,
		PermBillingPlanManage,
		PermBillingPlanRead,
		PermBillingPromoCodeManage,
		PermBillingWebhookRead,
		// plugins
			PermPluginsRead,
			PermPluginsInstall,
			PermPluginsUninstall,
			PermPluginsPermissionApprove,
		},
		IsSystem: true,
	},
	{
		Slug:        RoleTenantMember,
		Name:        "Tenant Member",
		Description: "Read everything and create/manage own resources. Cannot manage members or roles.",
		Permissions: []string{
			PermTenantRead,
			// no member management
			PermAuditRead,
			// no role management
			PermAuthPatManage,
			PermAuthSessionCreate,
			PermAuthSessionRevoke,
			// compute (no delete)
			PermComputeInstanceCreate,
			PermComputeInstanceRead,
			PermComputeInstanceUpdate,
			PermComputeInstanceStart,
			PermComputeInstanceStop,
			PermComputeInstanceRestart,
			PermComputeInstanceConsoleVNC,
			PermComputeProfileRead,
			PermComputeProfileApply,
			PermComputeImageRead,
			PermComputeNetworkRead,
			PermComputeStoragePoolRead,
			// compute snapshots / backups / schedules (WS-25, member =
			// create + read; deletes + target management are admin-only).
			PermComputeSnapshotCreate,
			PermComputeSnapshotRead,
			PermComputeBackupRead,
			PermComputeBackupRestore,
			PermComputeSnapshotPolicyRead,
			// compute cluster (WS-26, member = list + migrate own;
			// evacuate/restore is admin-only).
			PermComputeClusterMemberList,
			PermComputeInstanceMigrate,
			// dns (no zone delete)
			PermDNSZoneCreate,
			PermDNSZoneRead,
			PermDNSZoneUpdate,
			PermDNSRecordCreate,
			PermDNSRecordRead,
			PermDNSRecordUpdate,
			PermDNSRecordDelete,
			// s3 (no bucket delete)
			PermS3BucketCreate,
			PermS3BucketRead,
			PermS3BucketUpdate,
			PermS3ObjectRead,
			PermS3ObjectDelete,
			PermS3CredentialsCreate,
			PermS3CredentialsRevoke,
		// billing (own only — no balance.adjust)
		PermBillingBalanceRead,
		PermBillingLedgerRead,
		PermBillingReceiptRead,
		PermBillingPriceCatalogRead,
		// billing payments + subscriptions (WS-27, member = own only)
		PermBillingPaymentMethodManage,
		PermBillingPaymentIntentCreate,
		PermBillingSubscriptionManage,
		PermBillingPromoCodeRedeem,
		PermBillingPlanRead,
		// plugins (read-only)
			PermPluginsRead,
		},
		IsSystem: true,
	},
	{
		Slug:        RoleTenantViewer,
		Name:        "Tenant Viewer",
		Description: "Read-only access to everything in the tenant.",
		Permissions: []string{
			PermTenantRead,
			PermAuditRead,
			// compute reads
			PermComputeInstanceRead,
			PermComputeProfileRead,
			PermComputeImageRead,
			PermComputeNetworkRead,
			PermComputeStoragePoolRead,
			// compute snapshots / backups / schedules (WS-25 reads)
			PermComputeSnapshotRead,
			PermComputeBackupRead,
			PermComputeBackupTargetRead,
			PermComputeSnapshotPolicyRead,
			// compute cluster (WS-26 read: viewer can see members)
			PermComputeClusterMemberList,
			// dns reads
			PermDNSZoneRead,
			PermDNSRecordRead,
			// s3 reads
			PermS3BucketRead,
			PermS3ObjectRead,
		// billing reads
		PermBillingBalanceRead,
		PermBillingLedgerRead,
		PermBillingReceiptRead,
		PermBillingPriceCatalogRead,
		// billing payments + subscriptions (WS-27 reads)
		PermBillingPlanRead,
		// plugins reads
			PermPluginsRead,
		},
		IsSystem: true,
	},
}

// DefaultRoles returns a copy of the default role registry.
func DefaultRoles() []RoleDefinition {
	out := make([]RoleDefinition, len(defaultRoles))
	copy(out, defaultRoles)
	return out
}

// IsSystemRole reports whether slug is one of the Lahijan-seeded system roles
// that cannot be deleted by tenant admins.
func IsSystemRole(slug string) bool {
	for _, r := range defaultRoles {
		if r.Slug == slug {
			return r.IsSystem
		}
	}
	return false
}

// allPermissionSlugs returns every slug in the permission registry (used to
// seed the platform.admin role which bypasses checks anyway).
func allPermissionSlugs() []string {
	out := make([]string, 0, len(allPermissions))
	for _, p := range allPermissions {
		out = append(out, p.Slug)
	}
	return out
}

// allPermissionSlugsExceptGlobal returns every slug except the platform.*
// ones (those are reserved for the platform.admin role).
func allPermissionSlugsExceptGlobal() []string {
	out := make([]string, 0, len(allPermissions))
	for _, p := range allPermissions {
		if !startsWith(p.Slug, "platform.") {
			out = append(out, p.Slug)
		}
	}
	return out
}

// startsWith is a tiny local helper so this file doesn't import strings just
// for one call.
func startsWith(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}
