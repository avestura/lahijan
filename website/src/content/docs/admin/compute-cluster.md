---
title: Compute cluster
description: Run Lahijan against an Incus cluster, list and drain cluster members, move instances between them and manage public IP pools.
---

Lahijan can place instances across the members of an Incus cluster and gives operators API endpoints to see the members, drain one for maintenance and move instances between them. This page documents those endpoints and the operator-only IP pool endpoints under `/api/v1/admin/compute`. There is no dashboard page for them in this release, so you call them with the REST API.

## Placement modes

The setting `providers.incus.placement.mode` decides how new instances are placed:

| Mode              | Behaviour                                                                                                                                                                                                                                                                                                               |
| ----------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `local` (default) | Lahijan passes no target member; the Incus daemon decides. This is the single-node setup.                                                                                                                                                                                                                               |
| `cluster`         | For every new instance Lahijan lists the cluster members, drops those that are `Offline` or `Evacuated`, and picks the one with the highest free-capacity score. The choice is made under a per-tenant PostgreSQL advisory lock so several Lahijan replicas do not race. The chosen member is stored with the instance. |

The free-capacity score is the sum of two values Lahijan reads from each member's Incus config: `lahijan.free_cpu_mhz` and `lahijan.free_ram_mb`. Lahijan does not set these values itself. Without them every member scores 0, and Lahijan picks the member whose name sorts first, every time. If none are eligible, instance creation fails with `503`.

### Turn on cluster mode

1. Build the Incus cluster first, following the Incus clustering documentation. All members must run the same Incus version.
2. Make sure the `lahijan` container can reach the cluster, through the Unix socket of a member (`providers.incus.socketPath`) or through `providers.incus.remoteURL` with the `providers.incus.tls.*` client certificate settings.
3. Set the mode on the `lahijan` service. The line `LAHIJAN_PROVIDERS_INCUS_PLACEMENT_MODE` in `.env.prod.example` is not passed to the container by the compose file, so use an override:

   ```yaml title="deployments/docker-compose.override.yml"
   services:
     lahijan:
       environment:
         LAHIJAN_PROVIDERS_INCUS_PLACEMENT_MODE: "cluster"
   ```

4. Recreate the container and check the log line `compute placement driver wired` with `mode=cluster`:

   ```sh
   docker compose --env-file deployments/.env.prod \
     -f deployments/docker-compose.prod.yml -f deployments/docker-compose.override.yml \
     up -d --force-recreate lahijan
   docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml logs lahijan | grep "placement driver"
   ```

## Calling the endpoints

All endpoints need an authenticated user and a tenant context. From a script, use a personal access token and the tenant header:

```sh
curl -sS https://app.example.com/api/v1/compute/cluster/members \
  -H "Authorization: Bearer lah_pat_..." \
  -H "X-Tenant-Id: 7b1c0f7e-0000-4000-8000-000000000000"
```

See [Access tokens](/docs/account/access-tokens) for creating a token. Every endpoint answers `501` when the compute provider is disabled.

## Cluster members

| Method and path                                              | Permission                        | Purpose                             |
| ------------------------------------------------------------ | --------------------------------- | ----------------------------------- |
| `GET /api/v1/compute/cluster/members`                        | `compute.cluster.member.list`     | List members and their status       |
| `GET /api/v1/compute/cluster/members/{memberName}`           | `compute.cluster.member.list`     | One member                          |
| `POST /api/v1/compute/cluster/members/{memberName}/evacuate` | `compute.cluster.member.evacuate` | Drain a member                      |
| `POST /api/v1/compute/cluster/members/{memberName}/restore`  | `compute.cluster.member.evacuate` | Bring a drained member back         |
| `POST /api/v1/compute/instances/{instanceId}/migrate`        | `compute.instance.migrate`        | Move one instance to another member |

The member list reads the Incus cluster API directly. On a daemon that is not clustered, the answer depends on what Incus returns for its cluster members.

A member looks like this:

```json
{
	"serverName": "node-1",
	"url": "https://10.0.0.11:8443",
	"database": true,
	"status": "Online",
	"message": "Fully operational",
	"roles": ["database"],
	"architecture": "x86_64",
	"failureDomain": "default",
	"description": ""
}
```

`status` is `Online`, `Offline` or `Evacuated`.

### Drain and restore a member

Evacuating asks Incus to move the member's instances to other members and mark it `Evacuated`, so cluster placement skips it. Restoring makes it available again.

```sh
curl -sS -X POST https://app.example.com/api/v1/compute/cluster/members/node-2/evacuate \
  -H "Authorization: Bearer lah_pat_..." -H "X-Tenant-Id: <tenant-id>"
```

```json
{ "operationId": "5b7f0c3e-...", "memberName": "node-2", "action": "evacuate" }
```

- The call starts an asynchronous Incus operation and returns its id. Poll `GET /api/v1/compute/cluster/members/node-2` until `status` changes.
- Lahijan does not send an evacuation mode, so Incus uses the member's default behaviour.
- Each call writes an audit entry (`compute.cluster.member.evacuate` or `compute.cluster.member.restore`) before the request goes to Incus, and records the result.

> [!WARNING]
> Try evacuate and restore on a member that runs no important workloads before you rely on them in production, and watch the member with `incus cluster list` inside the `incus` container while the operation runs.

### Move an instance

```sh
curl -sS -X POST https://app.example.com/api/v1/compute/instances/<instance-id>/migrate \
  -H "Authorization: Bearer lah_pat_..." -H "X-Tenant-Id: <tenant-id>" \
  -H "Content-Type: application/json" \
  -d '{"targetMember": "node-3", "live": false}'
```

| Field          | Required            | Meaning                                                                                                |
| -------------- | ------------------- | ------------------------------------------------------------------------------------------------------ |
| `targetMember` | Yes                 | `serverName` of the destination member.                                                                |
| `live`         | No, default `false` | `true` asks Incus for a live migration. With `false` the instance is stopped, moved and started again. |
| `storagePool`  | No                  | Accepted by the API schema but not used in this release.                                               |

On success the response is the updated instance, with its member recorded from Incus. In `local` placement mode the call fails with `409` because migration is not supported there. The migration is recorded in the audit log with the requested target, the `live` flag and the resulting member.

## IP pools

IP pools are the operator side of [floating IPs](/docs/compute/floating-ips). You register public address ranges here; tenants allocate addresses from active pools.

| Method and path                                                   | Purpose                                                                  |
| ----------------------------------------------------------------- | ------------------------------------------------------------------------ |
| `GET /api/v1/admin/compute/ip-pools`                              | List pools (`limit`, `offset`)                                           |
| `POST /api/v1/admin/compute/ip-pools`                             | Create a pool                                                            |
| `GET /api/v1/admin/compute/ip-pools/{poolId}`                     | Get a pool                                                               |
| `PATCH /api/v1/admin/compute/ip-pools/{poolId}`                   | Change `description`, `ptrZoneId` or `isActive` (the name cannot change) |
| `DELETE /api/v1/admin/compute/ip-pools/{poolId}`                  | Delete a pool; refused with `409` while addresses are allocated          |
| `GET /api/v1/admin/compute/ip-pools/{poolId}/ranges`              | List ranges                                                              |
| `POST /api/v1/admin/compute/ip-pools/{poolId}/ranges`             | Add a CIDR range                                                         |
| `DELETE /api/v1/admin/compute/ip-pools/{poolId}/ranges/{rangeId}` | Remove a range; existing allocations stay valid                          |

All of them require `compute.ip_pool.manage`.

```sh
# Create a pool
curl -sS -X POST https://app.example.com/api/v1/admin/compute/ip-pools \
  -H "Authorization: Bearer lah_pat_..." -H "X-Tenant-Id: <tenant-id>" \
  -H "Content-Type: application/json" \
  -d '{"name": "public-v4", "description": "Upstream block", "isActive": true}'

# Add a range, keeping the gateway out of allocation
curl -sS -X POST https://app.example.com/api/v1/admin/compute/ip-pools/<pool-id>/ranges \
  -H "Authorization: Bearer lah_pat_..." -H "X-Tenant-Id: <tenant-id>" \
  -H "Content-Type: application/json" \
  -d '{"cidr": "203.0.113.0/28", "family": 4, "excludedAddresses": ["203.0.113.1"]}'
```

`cidr` must be in canonical form and `family` (4 or 6) must match it. `ptrZoneId` names a DNS zone for reverse records of addresses in the pool.

Lahijan only records allocations. Routing the addresses to your host (static route or BGP) is your job. When `providers.incus.floatingIPs.forwardNetwork` names a managed Incus network that owns those addresses, Lahijan also creates an Incus network forward for each attached address; when it is empty, attachments are recorded with the forward status `unsupported`.

## Permissions

`platform.admin` passes every permission check. The seeded tenant roles also carry some of these permissions:

| Permission                        | Roles                                                            |
| --------------------------------- | ---------------------------------------------------------------- |
| `compute.cluster.member.list`     | `tenant.viewer`, `tenant.member`, `tenant.admin`, `tenant.owner` |
| `compute.instance.migrate`        | `tenant.member`, `tenant.admin`, `tenant.owner`                  |
| `compute.cluster.member.evacuate` | `tenant.admin`, `tenant.owner`                                   |
| `compute.ip_pool.manage`          | `tenant.owner`                                                   |

`tenant.owner` receives every permission that does not start with `platform.`, which is why it holds the last two. Because self-registered users own a personal tenant by default, review this before you open registration to the public. See [Roles and permissions](/docs/admin/roles-and-permissions) and [Security hardening](/docs/operations/security#accounts-and-roles).
