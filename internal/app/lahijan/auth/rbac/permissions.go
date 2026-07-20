// Package rbac is Lahijan's single source of truth for permission slugs and
// default role bundles (WS-08). The permission registry below is the canonical
// list every service, middleware, and doc generator references; it is also the
// seed for the `permissions` and `role_permissions` tables.
//
// Permission slugs follow "scope.action" (per docs/glossary.md) where the
// scope is a module name (compute, dns, s3, billing, plugins, audit, rbac,
// tenant, auth, platform) and the action is verb.target (e.g.
// "instance.create", "zone.delete"). The slug is the ONLY thing stored in
// the DB; the description and i18n key live here so they cannot drift from
// the code that checks them.
//
// Default roles (per WS-08 doc):
//
//	tenant.owner   — full access within a tenant
//	tenant.admin   — manage resources + members, cannot transfer ownership
//	tenant.member  — read everything + create/manage own resources
//	tenant.viewer  — read-only
//
// RequirePerm is wired by the api/middleware package; this package only owns
// the catalog and the policy evaluator.
package rbac

// Permission is a registry entry: the slug (the value stored everywhere) plus
// a short English description used to seed the DB and render docs. The slug
// matches an i18n key "audit.action_<slug-with-dots-as-underscores>" that
// renders the localised label.
type Permission struct {
	Slug        string
	Description string
}

// Permission slugs grouped by module. Each constant is exported so callers can
// reference rbac.PermComputeInstanceCreate instead of typosprone string
// literals in RequirePerm calls. The list MUST stay in sync with
// allPermissions below (the registry uses the same slugs); a unit test
// enforces the invariant.
const (
	// --- auth (self-service; most do not require RequirePerm) ---
	PermAuthSessionCreate = "auth.session.create" // login
	PermAuthSessionRevoke = "auth.session.revoke" // logout / revoke other sessions
	PermAuthPatManage     = "auth.pat.manage"     // create/revoke own PATs

	// --- tenant management ---
	PermTenantRead             = "tenant.read"
	PermTenantUpdate           = "tenant.update"
	PermTenantDelete           = "tenant.delete"
	PermTenantMemberList       = "tenant.member.list"
	PermTenantMemberInvite     = "tenant.member.invite"
	PermTenantMemberRemove     = "tenant.member.remove"
	PermTenantMemberRoleUpdate = "tenant.member.role.update"

	// --- audit ---
	PermAuditRead       = "audit.read"        // read own tenant's audit
	PermAuditReadGlobal = "audit.read_global" // read across tenants (superuser)
	PermAuditExport     = "audit.export"

	// --- rbac management ---
	PermRBACRoleCreate = "rbac.role.create"
	PermRBACRoleUpdate = "rbac.role.update"
	PermRBACRoleDelete = "rbac.role.delete"
	PermRBACRoleList   = "rbac.role.list"

	// --- compute (lands with WS-14; declared now so PATs and docs can ref) ---
	PermComputeInstanceCreate  = "compute.instance.create"
	PermComputeInstanceRead    = "compute.instance.read"
	PermComputeInstanceUpdate  = "compute.instance.update"
	PermComputeInstanceStart   = "compute.instance.start"
	PermComputeInstanceStop    = "compute.instance.stop"
	PermComputeInstanceRestart = "compute.instance.restart"
	PermComputeInstanceDelete  = "compute.instance.delete"
	// PermComputeInstanceConsoleVNC opens a graphical (noVNC) console
	// session to a running virtual-machine instance. Distinct from exec
	// (WS-14) per WS-24: VM-only, RFB protocol, audit action
	// compute.instance.console.vnc.connect.
	PermComputeInstanceConsoleVNC = "compute.instance.console.vnc"
	PermComputeProfileRead        = "compute.profile.read"
	PermComputeProfileApply       = "compute.profile.apply"
	PermComputeImageRead          = "compute.image.read"
	PermComputeNetworkRead        = "compute.network.read"
	PermComputeNetworkCreate      = "compute.network.create"
	PermComputeStoragePoolRead    = "compute.storage_pool.read"

	// --- dns (WS-15) ---
	PermDNSZoneCreate   = "dns.zone.create"
	PermDNSZoneRead     = "dns.zone.read"
	PermDNSZoneUpdate   = "dns.zone.update"
	PermDNSZoneDelete   = "dns.zone.delete"
	PermDNSRecordCreate = "dns.record.create"
	PermDNSRecordRead   = "dns.record.read"
	PermDNSRecordUpdate = "dns.record.update"
	PermDNSRecordDelete = "dns.record.delete"

	// --- object storage / S3 (WS-16) ---
	PermS3BucketCreate      = "s3.bucket.create"
	PermS3BucketRead        = "s3.bucket.read"
	PermS3BucketUpdate      = "s3.bucket.update"
	PermS3BucketDelete      = "s3.bucket.delete"
	PermS3ObjectRead        = "s3.object.read"
	PermS3ObjectDelete      = "s3.object.delete"
	PermS3CredentialsCreate = "s3.credentials.create"
	PermS3CredentialsRevoke = "s3.credentials.revoke"

	// --- billing & metering (WS-17) ---
	PermBillingBalanceRead        = "billing.balance.read"
	PermBillingBalanceAdjust      = "billing.balance.adjust" // admin top-up / debit
	PermBillingLedgerRead         = "billing.ledger.read"
	PermBillingReceiptRead        = "billing.receipt.read"
	PermBillingReceiptCreate      = "billing.receipt.create"
	PermBillingPriceCatalogRead   = "billing.price_catalog.read"
	PermBillingPriceCatalogUpdate = "billing.price_catalog.update"

	// --- plugins (WS-10) ---
	PermPluginsRead              = "plugins.read"
	PermPluginsInstall           = "plugins.install"
	PermPluginsUninstall         = "plugins.uninstall"
	PermPluginsPermissionApprove = "plugins.permission.approve"

	// --- platform-level (superuser only) ---
	PermPlatformUserList     = "platform.user.list"
	PermPlatformTenantCreate = "platform.tenant.create"
	PermPlatformTenantDelete = "platform.tenant.delete"

	// --- platform jobs (WS-09) — admin-only River queue inspection/control.
	// Granted to platform.admin via allPermissionSlugs(); every other role
	// is excluded via allPermissionSlugsExceptGlobal so tenant-local admins
	// cannot retry/cancel jobs from other tenants. ---
	PermPlatformJobsRead   = "platform.jobs.read"
	PermPlatformJobsRetry  = "platform.jobs.retry"
	PermPlatformJobsCancel = "platform.jobs.cancel"
)

// allPermissions is the single source of truth for the permissions table seed.
// Add new permissions here; the seeder picks them up automatically. The order
// is grouped by module for readability; the DB orders by slug.
//
// PLEASE keep these alphabetized within each module so diffs are reviewable.
var allPermissions = []Permission{
	// auth
	{Slug: PermAuthPatManage, Description: "Manage own personal access tokens."},
	{Slug: PermAuthSessionCreate, Description: "Open a session (log in)."},
	{Slug: PermAuthSessionRevoke, Description: "Revoke a session (own or other's within the tenant)."},

	// audit
	{Slug: PermAuditExport, Description: "Export the tenant audit log (CSV/JSON)."},
	{Slug: PermAuditRead, Description: "Read the tenant audit log."},
	{Slug: PermAuditReadGlobal, Description: "Read audit log across all tenants (superuser)."},

	// rbac
	{Slug: PermRBACRoleCreate, Description: "Create a custom role in the tenant."},
	{Slug: PermRBACRoleDelete, Description: "Delete a custom role in the tenant."},
	{Slug: PermRBACRoleList, Description: "List roles and their permissions."},
	{Slug: PermRBACRoleUpdate, Description: "Update a role's permission bundle."},

	// tenant
	{Slug: PermTenantDelete, Description: "Delete the tenant."},
	{Slug: PermTenantMemberInvite, Description: "Invite a user into the tenant."},
	{Slug: PermTenantMemberList, Description: "List members of the tenant."},
	{Slug: PermTenantMemberRemove, Description: "Remove a member from the tenant."},
	{Slug: PermTenantMemberRoleUpdate, Description: "Change a member's role."},
	{Slug: PermTenantRead, Description: "View tenant metadata."},
	{Slug: PermTenantUpdate, Description: "Update tenant metadata."},

	// compute
	{Slug: PermComputeImageRead, Description: "List and inspect instance images."},
	{Slug: PermComputeInstanceConsoleVNC, Description: "Open a graphical (noVNC) console session to a running virtual-machine instance."},
	{Slug: PermComputeInstanceCreate, Description: "Create an instance."},
	{Slug: PermComputeInstanceDelete, Description: "Delete an instance."},
	{Slug: PermComputeInstanceRead, Description: "View instances."},
	{Slug: PermComputeInstanceRestart, Description: "Restart an instance."},
	{Slug: PermComputeInstanceStart, Description: "Start an instance."},
	{Slug: PermComputeInstanceStop, Description: "Stop an instance."},
	{Slug: PermComputeInstanceUpdate, Description: "Update an instance's config."},
	{Slug: PermComputeNetworkCreate, Description: "Create a tenant network."},
	{Slug: PermComputeNetworkRead, Description: "View tenant networks."},
	{Slug: PermComputeProfileApply, Description: "Apply a profile to an instance."},
	{Slug: PermComputeProfileRead, Description: "View instance profiles."},
	{Slug: PermComputeStoragePoolRead, Description: "View storage pools."},

	// dns
	{Slug: PermDNSRecordCreate, Description: "Create a DNS record."},
	{Slug: PermDNSRecordDelete, Description: "Delete a DNS record."},
	{Slug: PermDNSRecordRead, Description: "View DNS records."},
	{Slug: PermDNSRecordUpdate, Description: "Update a DNS record."},
	{Slug: PermDNSZoneCreate, Description: "Create a DNS zone."},
	{Slug: PermDNSZoneDelete, Description: "Delete a DNS zone."},
	{Slug: PermDNSZoneRead, Description: "View DNS zones."},
	{Slug: PermDNSZoneUpdate, Description: "Update a DNS zone's settings."},

	// s3 / object storage
	{Slug: PermS3BucketCreate, Description: "Create an S3 bucket."},
	{Slug: PermS3BucketDelete, Description: "Delete an S3 bucket."},
	{Slug: PermS3BucketRead, Description: "View S3 buckets."},
	{Slug: PermS3BucketUpdate, Description: "Update an S3 bucket's policy or lifecycle."},
	{Slug: PermS3CredentialsCreate, Description: "Mint S3 credentials for the user."},
	{Slug: PermS3CredentialsRevoke, Description: "Revoke S3 credentials."},
	{Slug: PermS3ObjectDelete, Description: "Delete S3 objects."},
	{Slug: PermS3ObjectRead, Description: "Read S3 objects."},

	// billing
	{Slug: PermBillingBalanceAdjust, Description: "Adjust a user's balance (admin top-up/debit)."},
	{Slug: PermBillingBalanceRead, Description: "View a user's balance."},
	{Slug: PermBillingLedgerRead, Description: "View the user's ledger entries."},
	{Slug: PermBillingPriceCatalogRead, Description: "View the price catalog."},
	{Slug: PermBillingPriceCatalogUpdate, Description: "Update the price catalog."},
	{Slug: PermBillingReceiptCreate, Description: "Generate a receipt."},
	{Slug: PermBillingReceiptRead, Description: "View receipts."},

	// plugins
	{Slug: PermPluginsInstall, Description: "Install a plugin."},
	{Slug: PermPluginsPermissionApprove, Description: "Approve a plugin's requested permissions."},
	{Slug: PermPluginsRead, Description: "View installed plugins."},
	{Slug: PermPluginsUninstall, Description: "Uninstall a plugin."},

	// platform
	{Slug: PermPlatformJobsCancel, Description: "Cancel any queued or running job (platform admin)."},
	{Slug: PermPlatformJobsRead, Description: "Inspect every queued/running/failed job across tenants (platform admin)."},
	{Slug: PermPlatformJobsRetry, Description: "Manually retry a discarded/DLQ'd job (platform admin)."},
	{Slug: PermPlatformTenantCreate, Description: "Create a tenant (platform admin)."},
	{Slug: PermPlatformTenantDelete, Description: "Delete any tenant (platform admin)."},
	{Slug: PermPlatformUserList, Description: "List all users (platform admin)."},
}

// AllPermissions returns a copy of the registry. Callers must not mutate the
// returned slice; tests use it to assert the DB seed matches the code.
func AllPermissions() []Permission {
	out := make([]Permission, len(allPermissions))
	copy(out, allPermissions)
	return out
}

// PermissionExists reports whether slug is in the registry. Used by the
// PAT-issuance path to reject requests for unknown permissions.
func PermissionExists(slug string) bool {
	for _, p := range allPermissions {
		if p.Slug == slug {
			return true
		}
	}
	return false
}
