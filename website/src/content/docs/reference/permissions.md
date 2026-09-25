---
title: Permissions
description: Every permission string in Lahijan, what it allows, which built-in roles hold it and which API calls check it.
---

This is the complete permission catalog: all 109 permissions Lahijan seeds into its database. For how permissions are evaluated, read [Roles and permissions](/docs/admin/roles-and-permissions).

## How to read the tables

The **Roles** column uses these letters for the built-in tenant roles:

| Letter | Role            |
| ------ | --------------- |
| O      | `tenant.owner`  |
| A      | `tenant.admin`  |
| M      | `tenant.member` |
| V      | `tenant.viewer` |

`platform.admin` is not listed: it holds every permission and passes every check in the tenant where it is held. A dash means no tenant role holds the permission.

The **Checked on** column lists the API calls that require the permission. Paths are relative to `/api/v1`. "Not checked yet" means the permission exists in the catalog, and in role grants, but no endpoint requires it in this release.

Endpoints that act only on your own account (`/auth/me`, `/auth/personal-access-tokens`, `/me/mfa/*`, `/me/identities`) need a signed-in user but no permission. `GET /billing/config`, `GET /billing/plans` and `POST /webhooks/stripe` are not permission-gated.

## Account and session

| Permission            | Allows                                  | Roles | Checked on      |
| --------------------- | --------------------------------------- | ----- | --------------- |
| `auth.pat.manage`     | Manage your own personal access tokens. | O A M | Not checked yet |
| `auth.session.create` | Open a session (sign in).               | O A M | Not checked yet |
| `auth.session.revoke` | Revoke a session.                       | O A M | Not checked yet |

## Tenant and roles

| Permission                  | Allows                            | Roles   | Checked on      |
| --------------------------- | --------------------------------- | ------- | --------------- |
| `tenant.read`               | View tenant details.              | O A M V | Not checked yet |
| `tenant.update`             | Update tenant details.            | O A     | Not checked yet |
| `tenant.delete`             | Delete the tenant.                | O       | Not checked yet |
| `tenant.member.list`        | List members of the tenant.       | O A     | Not checked yet |
| `tenant.member.invite`      | Invite a user into the tenant.    | O A     | Not checked yet |
| `tenant.member.remove`      | Remove a member from the tenant.  | O A     | Not checked yet |
| `tenant.member.role.update` | Change a member's role.           | O A     | Not checked yet |
| `rbac.role.list`            | List roles and their permissions. | O A     | Not checked yet |
| `rbac.role.create`          | Create a custom role.             | O A     | Not checked yet |
| `rbac.role.update`          | Change a role's permissions.      | O A     | Not checked yet |
| `rbac.role.delete`          | Delete a custom role.             | O A     | Not checked yet |

## Audit

| Permission          | Allows                                      | Roles   | Checked on                                                                   |
| ------------------- | ------------------------------------------- | ------- | ---------------------------------------------------------------------------- |
| `audit.read`        | Read the tenant audit log.                  | O A M V | `GET /audit`, `GET /audit/{auditId}`; the Agent's `audit.list_events` lookup |
| `audit.export`      | Export the tenant audit log as CSV or JSON. | O A     | `GET /audit/export`                                                          |
| `audit.read_global` | Read the audit log across all tenants.      | O       | Not checked yet                                                              |

## Compute: instances, images, profiles

| Permission                      | Allows                                                 | Roles   | Checked on                                                                                                                                          |
| ------------------------------- | ------------------------------------------------------ | ------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| `compute.instance.read`         | View instances.                                        | O A M V | `GET /compute/instances`, `GET /compute/instances/{instanceId}` and its `runtime` and `logs` sub-paths; the Agent's `compute.list_instances` lookup |
| `compute.instance.create`       | Create an instance.                                    | O A M   | `POST /compute/instances`, `POST /compute/images` (image upload)                                                                                    |
| `compute.instance.update`       | Change an instance's configuration.                    | O A M   | `PATCH /compute/instances/{instanceId}`, `POST /compute/instances/{instanceId}/snapshots/{snapshotId}/restore`                                      |
| `compute.instance.start`        | Start an instance.                                     | O A M   | `POST /compute/instances/{instanceId}/start`, `POST /compute/instances/{instanceId}/exec`                                                           |
| `compute.instance.stop`         | Stop an instance.                                      | O A M   | `POST /compute/instances/{instanceId}/stop`, `.../freeze`, `.../unfreeze`                                                                           |
| `compute.instance.restart`      | Restart an instance.                                   | O A M   | `POST /compute/instances/{instanceId}/restart`                                                                                                      |
| `compute.instance.delete`       | Delete an instance.                                    | O A     | `DELETE /compute/instances/{instanceId}`, `DELETE /compute/images/{imageId}`                                                                        |
| `compute.instance.console.exec` | Open an interactive text shell on a running instance.  | O A M   | `GET /compute/instances/{instanceId}/console`                                                                                                       |
| `compute.instance.console.vnc`  | Open a graphical console on a running virtual machine. | O A M   | `GET /compute/instances/{instanceId}/vnc`                                                                                                           |
| `compute.instance.migrate`      | Move an instance to another cluster member.            | O A M   | `POST /compute/instances/{instanceId}/migrate`                                                                                                      |
| `compute.image.read`            | List and inspect images.                               | O A M V | `GET /compute/images`, `GET /compute/images/{imageId}`                                                                                              |
| `compute.profile.read`          | View profiles.                                         | O A M V | `GET /compute/profiles`, `GET /compute/profiles/{profileId}`                                                                                        |
| `compute.profile.apply`         | Create, apply and delete profiles.                     | O A M   | `POST /compute/profiles`, `DELETE /compute/profiles/{profileId}`                                                                                    |

## Compute: networks and storage

| Permission                   | Allows                                             | Roles   | Checked on                                                                                                                    |
| ---------------------------- | -------------------------------------------------- | ------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `compute.network.read`       | View tenant networks.                              | O A M V | `GET /compute/networks`, `GET /compute/networks/{networkId}`                                                                  |
| `compute.network.create`     | Create and delete tenant networks.                 | O A     | `POST /compute/networks`, `DELETE /compute/networks/{networkId}`, `POST /compute/storage`                                     |
| `compute.storage_pool.read`  | View storage.                                      | O A M V | `GET /compute/storage`, `GET /compute/storage/{volumeId}`, `DELETE /compute/storage/{volumeId}`                               |
| `compute.floating_ip.read`   | View the tenant's floating IPs.                    | O A M V | `GET /compute/floating-ips`, `GET /compute/floating-ips/{floatingIpId}`, `GET /compute/instances/{instanceId}/floating-ip`    |
| `compute.floating_ip.manage` | Allocate, attach, detach and release floating IPs. | O A M   | `POST /compute/floating-ips`, `PATCH` and `DELETE /compute/floating-ips/{floatingIpId}`, `POST .../attach`, `POST .../detach` |
| `compute.ip_pool.manage`     | Manage the operator's IP pools and ranges.         | O       | Every method on `/admin/compute/ip-pools` and its sub-paths                                                                   |

> [!WARNING]
> In this release `DELETE /compute/storage/{volumeId}` is gated by the read permission `compute.storage_pool.read`, which every tenant role holds, and `POST /compute/storage` is gated by `compute.network.create`. `compute.ip_pool.manage` is held by `tenant.owner` even though IP pools are shared across tenants.

## Compute: snapshots, backups, schedules

| Permission                       | Allows                                       | Roles   | Checked on                                                                                               |
| -------------------------------- | -------------------------------------------- | ------- | -------------------------------------------------------------------------------------------------------- |
| `compute.snapshot.read`          | View snapshots.                              | O A M V | `GET /compute/instances/{instanceId}/snapshots` and `.../snapshots/{snapshotId}`                         |
| `compute.snapshot.create`        | Take a snapshot.                             | O A M   | `POST /compute/instances/{instanceId}/snapshots`                                                         |
| `compute.snapshot.delete`        | Delete a snapshot.                           | O A     | `DELETE /compute/instances/{instanceId}/snapshots/{snapshotId}`                                          |
| `compute.snapshot_policy.read`   | View snapshot schedules.                     | O A M V | `GET /compute/snapshot-policies`, `GET /compute/snapshot-policies/{policyId}`                            |
| `compute.snapshot_policy.create` | Create a snapshot schedule.                  | O A     | `POST /compute/snapshot-policies`                                                                        |
| `compute.snapshot_policy.update` | Change a snapshot schedule.                  | O A     | `PATCH /compute/snapshot-policies/{policyId}`                                                            |
| `compute.snapshot_policy.delete` | Delete a snapshot schedule.                  | O A     | `DELETE /compute/snapshot-policies/{policyId}`                                                           |
| `compute.backup.read`            | View off-host backups.                       | O A M V | `GET /compute/backups`, `GET /compute/backups/{backupId}`, `GET /compute/instances/{instanceId}/backups` |
| `compute.backup.delete`          | Delete an off-host backup.                   | O A     | `DELETE /compute/backups/{backupId}`                                                                     |
| `compute.backup.restore`         | Restore an instance from an off-host backup. | O A M   | Not checked yet                                                                                          |
| `compute.backup.target.read`     | View backup targets.                         | O A V   | `GET /compute/backup-targets`, `GET /compute/backup-targets/{targetId}`                                  |
| `compute.backup.target.create`   | Create a backup target.                      | O A     | `POST /compute/backup-targets`                                                                           |
| `compute.backup.target.update`   | Change a backup target.                      | O A     | Not checked yet                                                                                          |
| `compute.backup.target.delete`   | Delete a backup target.                      | O A     | `DELETE /compute/backup-targets/{targetId}`                                                              |

## Compute: cluster

| Permission                        | Allows                                 | Roles   | Checked on                                                                  |
| --------------------------------- | -------------------------------------- | ------- | --------------------------------------------------------------------------- |
| `compute.cluster.member.list`     | List cluster members and their status. | O A M V | `GET /compute/cluster/members`, `GET /compute/cluster/members/{memberName}` |
| `compute.cluster.member.evacuate` | Evacuate or restore a cluster member.  | O A     | `POST /compute/cluster/members/{memberName}/evacuate`, `.../restore`        |

## DNS

| Permission            | Allows                                                              | Roles   | Checked on                                                                                                         |
| --------------------- | ------------------------------------------------------------------- | ------- | ------------------------------------------------------------------------------------------------------------------ |
| `dns.zone.read`       | View zones and zone templates.                                      | O A M V | `GET /dns/zones`, `GET /dns/zones/{zoneId}`, `GET /dns/templates`; the Agent's `dns.list_zones` lookup             |
| `dns.zone.create`     | Create a zone.                                                      | O A M   | `POST /dns/zones`                                                                                                  |
| `dns.zone.update`     | Change a zone, turn DNSSEC on or off, apply a template.             | O A M   | `PATCH /dns/zones/{zoneId}`, `POST /dns/zones/{zoneId}/dnssec/{action}`, `POST /dns/zones/{zoneId}/apply-template` |
| `dns.zone.delete`     | Delete a zone.                                                      | O A     | `DELETE /dns/zones/{zoneId}`                                                                                       |
| `dns.record.read`     | View records.                                                       | O A M V | `GET /dns/zones/{zoneId}/records`, `GET .../records/{recordId}`                                                    |
| `dns.record.create`   | Create a record.                                                    | O A M   | `POST /dns/zones/{zoneId}/records`                                                                                 |
| `dns.record.update`   | Change a record.                                                    | O A M   | `PATCH /dns/zones/{zoneId}/records/{recordId}`                                                                     |
| `dns.record.delete`   | Delete a record.                                                    | O A M   | `DELETE /dns/zones/{zoneId}/records/{recordId}`                                                                    |
| `dns.domain.search`   | Search for available domains.                                       | O A M V | `POST /dns/domains/search`                                                                                         |
| `dns.domain.read`     | View the tenant's domains.                                          | O A M V | `GET /dns/domains`, `GET /dns/domains/{domainId}`                                                                  |
| `dns.domain.register` | Register a domain (charges the balance).                            | O A M   | `POST /dns/domains`                                                                                                |
| `dns.domain.renew`    | Renew a domain (charges the balance).                               | O A M   | `POST /dns/domains/{domainId}/renew`                                                                               |
| `dns.domain.transfer` | Transfer a domain in (charges the balance).                         | O A M   | `POST /dns/domains/transfer`                                                                                       |
| `dns.domain.delete`   | Remove a domain from the tenant (does not cancel the registration). | O A     | `DELETE /dns/domains/{domainId}`                                                                                   |

## Object storage

| Permission              | Allows                                                     | Roles   | Checked on                                                                                                                                                                                                                         |
| ----------------------- | ---------------------------------------------------------- | ------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `s3.bucket.read`        | View buckets and their settings.                           | O A M V | `GET /storage/buckets`, `GET /storage/buckets/{bucketId}`, `GET .../credentials`, `GET .../usage`, `GET .../versioning`, `GET .../versions`, `GET .../lifecycle`, `GET .../object-lock`; the Agent's `storage.list_buckets` lookup |
| `s3.bucket.create`      | Create a bucket.                                           | O A M   | `POST /storage/buckets`                                                                                                                                                                                                            |
| `s3.bucket.update`      | Change a bucket, set its quota, restore an object version. | O A M   | `PATCH /storage/buckets/{bucketId}`, `POST .../quota`, `POST .../versions/restore`                                                                                                                                                 |
| `s3.bucket.delete`      | Delete a bucket.                                           | O A     | `DELETE /storage/buckets/{bucketId}`                                                                                                                                                                                               |
| `s3.bucket.versioning`  | Turn versioning on or off.                                 | O A     | `PUT /storage/buckets/{bucketId}/versioning`                                                                                                                                                                                       |
| `s3.bucket.lifecycle`   | Manage lifecycle rules.                                    | O A     | `POST`, `PUT`, `PATCH` and `DELETE` on `.../lifecycle` and `.../lifecycle/{ruleId}`, `POST .../lifecycle/{ruleId}/status`                                                                                                          |
| `s3.bucket.object_lock` | Set the object-lock policy.                                | O A     | `PUT /storage/buckets/{bucketId}/object-lock`                                                                                                                                                                                      |
| `s3.credentials.create` | Create access keys.                                        | O A M   | `POST /storage/buckets/{bucketId}/credentials`                                                                                                                                                                                     |
| `s3.credentials.revoke` | Revoke access keys.                                        | O A M   | `DELETE /storage/buckets/{bucketId}/credentials/{credentialId}`                                                                                                                                                                    |
| `s3.object.read`        | Read objects; create pre-signed download and upload URLs.  | O A M V | `POST /storage/buckets/{bucketId}/presign`                                                                                                                                                                                         |
| `s3.object.delete`      | Delete objects.                                            | O A M   | Not checked yet                                                                                                                                                                                                                    |

## Billing

| Permission                      | Allows                                             | Roles   | Checked on                                                                                                    |
| ------------------------------- | -------------------------------------------------- | ------- | ------------------------------------------------------------------------------------------------------------- |
| `billing.balance.read`          | View balances and usage.                           | O A M V | `GET /me/balance`, `GET /me/usage`, `GET /admin/users/{userId}/balance`                                       |
| `billing.balance.adjust`        | Top up, refund and rebuild balances.               | O A     | `POST /admin/users/{userId}/topup`, `POST /admin/users/{userId}/refund`, `POST /admin/users/{userId}/balance` |
| `billing.ledger.read`           | View ledger entries.                               | O A M V | `GET /me/ledger`, `GET /admin/users/{userId}/ledger`; the Agent's `billing.list_usage` lookup                 |
| `billing.receipt.read`          | View and download receipts.                        | O A M V | `GET /me/receipts`, `GET /me/receipts/{receiptId}`, `GET /me/receipts/{receiptId}.pdf`                        |
| `billing.receipt.create`        | Generate a receipt.                                | O A     | `POST /me/receipts`                                                                                           |
| `billing.price_catalog.read`    | View the price catalog.                            | O A M V | `GET /admin/billing/prices`                                                                                   |
| `billing.price_catalog.update`  | Set prices.                                        | O A     | `POST /admin/billing/prices`                                                                                  |
| `billing.payment_method.manage` | Manage your own cards.                             | O A M   | `GET` and `POST /billing/payment-methods`, `DELETE /billing/payment-methods/{paymentMethodId}`                |
| `billing.payment_intent.create` | Top up your own balance by card.                   | O A M   | `POST /billing/topup`                                                                                         |
| `billing.subscription.manage`   | Subscribe, list and cancel your own subscriptions. | O A M   | `GET` and `POST /billing/subscriptions`, `DELETE /billing/subscriptions/{subscriptionId}`                     |
| `billing.promo_code.redeem`     | Redeem a promo code.                               | O A M   | `POST /billing/redeem`                                                                                        |
| `billing.plan.read`             | View subscription plans (admin view).              | O A M V | `GET /admin/billing/plans`, `GET /admin/billing/plans/{planId}`                                               |
| `billing.plan.manage`           | Create, change, delete and push plans.             | O A     | `POST /admin/billing/plans`, `PATCH` and `DELETE /admin/billing/plans/{planId}`, `POST .../push`              |
| `billing.promo_code.manage`     | Create, list and revoke promo codes.               | O A     | `GET` and `POST /admin/billing/promo-codes`, `POST /admin/billing/promo-codes/{promoCodeId}/revoke`           |
| `billing.webhook.read`          | Read the Stripe webhook log.                       | O A     | `GET /admin/billing/webhook-events`                                                                           |

## Plugins

| Permission                   | Allows                                                      | Roles   | Checked on                                                                                                                                                                                                                                         |
| ---------------------------- | ----------------------------------------------------------- | ------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `plugins.read`               | Browse the plugin marketplace.                              | O A M V | `GET /admin/marketplace`, `GET /admin/marketplace/{name}`                                                                                                                                                                                          |
| `plugins.install`            | List, upload, install, upgrade, enable and disable plugins. | O A     | `GET /admin/plugins`, `GET /admin/plugins/{pluginId}`, `POST /admin/plugins/upload`, `POST /admin/plugins/install/{name}`, `POST /admin/plugins/upgrade/{name}`, `POST /admin/plugins/{pluginId}/enable`, `POST /admin/plugins/{pluginId}/disable` |
| `plugins.uninstall`          | Remove a plugin.                                            | O A     | `DELETE /admin/plugins/{pluginId}`                                                                                                                                                                                                                 |
| `plugins.permission.approve` | Approve or revoke a plugin's requested permissions.         | O A     | `POST /admin/plugins/{pluginId}/permissions/{permission}/{action}`                                                                                                                                                                                 |

Plugins also declare their own permissions in their manifest; those are separate from this catalog. See [Manifest reference](/docs/plugins/manifest).

## Agent

| Permission                  | Allows                                                   | Roles   | Checked on                                                                                   |
| --------------------------- | -------------------------------------------------------- | ------- | -------------------------------------------------------------------------------------------- |
| `agent.conversation.read`   | View your conversations; read the tenant's agent policy. | O A M V | `GET /agent/conversations`, `GET /agent/conversations/{conversationId}`, `GET /agent/policy` |
| `agent.conversation.create` | Start a conversation.                                    | O A M   | `POST /agent/conversations`                                                                  |
| `agent.conversation.delete` | Delete a conversation.                                   | O A M   | `DELETE /agent/conversations/{conversationId}`                                               |
| `agent.message.send`        | Send a message to the Agent.                             | O A M   | `POST /agent/conversations/{conversationId}/messages`                                        |
| `agent.tool.confirm`        | Approve or decline a pending Agent action.               | O A M   | `POST /agent/tool-calls/{toolCallId}/confirm`                                                |
| `agent.provider.manage`     | Manage your own model-provider keys.                     | O A M   | `GET` and `POST /agent/providers`, `DELETE /agent/providers/{providerId}`                    |
| `agent.policy.manage`       | Change the tenant's agent policy.                        | O A     | `PUT /agent/policy`                                                                          |

## Platform

These permissions are held only by `platform.admin`.

| Permission               | Allows                                              | Roles | Checked on                                                                         |
| ------------------------ | --------------------------------------------------- | ----- | ---------------------------------------------------------------------------------- |
| `platform.jobs.read`     | Inspect queued, running and failed background jobs. | -     | `GET /admin/jobs`, `GET /admin/jobs/{jobId}`, the job web page at `/admin/jobs/ui` |
| `platform.jobs.retry`    | Retry a failed job.                                 | -     | `POST /admin/jobs/{jobId}/retry`                                                   |
| `platform.jobs.cancel`   | Cancel a queued or running job.                     | -     | `POST /admin/jobs/{jobId}/cancel`                                                  |
| `platform.user.list`     | List all users.                                     | -     | Not checked yet                                                                    |
| `platform.tenant.create` | Create a tenant.                                    | -     | Not checked yet                                                                    |
| `platform.tenant.delete` | Delete any tenant.                                  | -     | Not checked yet                                                                    |
