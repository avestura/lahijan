# Glossary

The terms used across Lahijan docs. **Use these consistently** in API, UI, docs.

## Identity & tenancy

| Term | Meaning |
|------|---------|
| **Tenant** | The top-level isolation boundary. Every user-visible resource belongs to exactly one tenant. Maps to one Incus project. |
| **User** | A person who can log in. Belongs to one or more tenants. Has a unique email. |
| **Membership** | The link between a user and a tenant, carrying a role (e.g. owner, admin, member, viewer). |
| **Role** | A named bundle of permissions, scoped to a tenant (e.g. `tenant.admin`, `compute.operator`). |
| **Permission** | An atomic capability, formatted `scope.action` (e.g. `compute.instance.create`). |
| **Actor** | Whoever performed an action: a user, a plugin, or the system. Recorded in `audit_log.actor_type`. |

## Resources

| Term | Meaning |
|------|---------|
| **Instance** | A compute unit managed by Incus: a system container or a virtual machine. Never called "VM" or "container" in user-facing copy unless the user explicitly picked the type. |
| **Image** | A template used to create instances (e.g. `ubuntu/24.04`). Backed by Incus image server. |
| **Profile** | An Incus profile: a named bundle of config + devices applied to instances. |
| **Device** | An Incus device: nic, disk, gpu, proxy, etc. Attached via profile or directly. |
| **Zone** | A DNS zone (e.g. `example.com.`) served by PowerDNS. Belongs to a tenant. |
| **Record** | A DNS record within a zone (A, AAAA, CNAME, MX, TXT, ...). |
| **Bucket** | An S3 bucket on SeaweedFS. Belongs to a tenant. |
| **Object** | A single S3 object (file) in a bucket. Lahijan does not store object metadata. |
| **Plugin** | A WASM module installed by an admin. Declares a permission manifest. |

## Tables (database conventions)

| Table type | `tenant_id` column? | Examples |
|------------|---------------------|----------|
| **Tenant-scoped** (the common case) | `NOT NULL` | `instances`, `dns_zones`, `dns_records`, `buckets`, `ledger_entries`, `usage_events`, `audit_log` (for tenant events) |
| **Global** | none | `tenants`, `users`, `roles`, `permissions`, `role_permissions`, `user_roles`, `user_oauth_identities`, `user_saml_identities`, `user_totp_secrets`, `user_webauthn_credentials`, `refresh_tokens`, `personal_access_tokens`, `plugins`, `plugin_permissions`, `schema_migrations`, `river_*` |

**Hard rule:** when in doubt, add `tenant_id`. Removing it later is much easier
than adding it.

## Billing

| Term | Meaning |
|------|---------|
| **Ledger** | The append-only table of monetary movements per user. |
| **Ledger entry** | One row: credit (top-up) or debit (usage charge). Never UPDATEd. |
| **Balance** | The sum of a user's ledger entries at this moment. Cached; refreshed from the ledger. |
| **Usage event** | A metering record: tenant + resource + qty + minute timestamp. Source of truth for charges. |
| **Price catalog** | Per-resource unit prices (CPU-h, RAM-h, GB-month, ...) set by admin. |
| **Prepaid credit** | Money added to a user's balance before consumption. |
| **PAYG (pay-as-you-go)** | Money debited from balance after consumption, based on usage events. |
| **Receipt / Invoice** | PDF document summarizing charges over a period; generated from the ledger. |

## Infrastructure backends (internal jargon)

These names appear in code and operator docs but **never in user-facing UI or
public API** (per ADR-0001 / pillar 1).

| Internal | User-facing name |
|----------|------------------|
| Incus | "compute" / "instances" |
| PowerDNS | "dns" / "DNS" |
| SeaweedFS | "object storage" / "S3" |

## Linting & CI

| Term | Meaning |
|------|---------|
| **DoD** | Definition of Done. The checklist at the bottom of every workstream doc. |
| **WS** | Workstream. See `docs/workstreams/`. |
| **ADR** | Architecture Decision Record. See `docs/adr/`. |
| **Phase** | A dependency-ordered group of workstreams (0–7). |
