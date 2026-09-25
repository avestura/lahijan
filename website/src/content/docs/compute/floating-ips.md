---
title: Floating IPs
description: Allocate a public IP address from an operator pool, attach it to an instance, move it between instances and set its reverse DNS name.
---

A floating IP is a public IP address that belongs to your tenant rather than to one instance. You allocate it once, attach it to an instance, and later detach it and attach it to another instance without the address changing. Use it for anything that needs a stable public address, such as a web server or a mail server.

Floating IPs are managed through the API only; there is no dashboard page yet.

## How it works

- Addresses come from **IP pools** that the operator manages. You cannot create pools or see the pool list; ask your operator for the ID of the pool you should allocate from.
- An allocated address stays with your tenant until you release it, whether or not it is attached.
- An instance can have at most one floating IP, and a floating IP can be attached to one instance at a time.

## Allocate a floating IP

`POST /api/v1/compute/floating-ips` picks the next free address in a pool:

```sh
curl -X POST https://lahijan.example.com/api/v1/compute/floating-ips \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{
    "poolId": "3c9e1f2a-7b4d-4e8f-a1b2-c3d4e5f60718",
    "ptrTarget": "web.example.com."
  }'
```

- `poolId` is required.
- `ptrTarget` is optional. It is the host name that reverse DNS lookups of the address return. It must be lowercase; a trailing dot is added if you leave it out.

The response is `201`:

```json
{
	"id": "5f0c2b7e-1d3a-4c9b-8e6f-0a1b2c3d4e5f",
	"tenantId": "9b8a7c6d-5e4f-4a3b-2c1d-0e9f8a7b6c5d",
	"poolId": "3c9e1f2a-7b4d-4e8f-a1b2-c3d4e5f60718",
	"address": "203.0.113.25",
	"family": 4,
	"ptrTarget": "web.example.com.",
	"instanceId": null,
	"networkName": null,
	"forwardPushStatus": "pending",
	"createdAt": "2026-09-25T10:15:00Z"
}
```

A `409` means the pool is inactive or has no free addresses, or another request took the same address at the same moment. In the last case, retry. An unknown pool returns `404`.

## Attach it to an instance

```sh
curl -X POST https://lahijan.example.com/api/v1/compute/floating-ips/$FLOATING_IP_ID/attach \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{ "instanceId": "8a1d6c3e-5b7f-4e2a-9c0d-1f2e3a4b5c6d" }'
```

The instance must belong to your tenant. The call returns `409` if the floating IP is already attached, or if the instance already has one. If the operator has set up reverse DNS for the pool, the `ptrTarget` record is published when you attach.

Check `forwardPushStatus` in the response to see whether Lahijan routed the address to the instance:

| Value         | Meaning                                                                                                                    |
| ------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `pushed`      | Lahijan set up forwarding from the public address to the instance on the network named in `networkName`.                   |
| `unsupported` | This deployment routes public addresses outside Lahijan. The attach is recorded; your operator's network handles delivery. |
| `failed`      | Lahijan tried to set up forwarding and it failed. The attach is still recorded. Contact your operator.                     |
| `pending`     | No attach has been processed yet.                                                                                          |

> [!NOTE]
> An attach is recorded even when forwarding could not be set up. If traffic does not reach your instance, check `forwardPushStatus` first.

To find the floating IP attached to an instance, call `GET /api/v1/compute/instances/{instanceId}/floating-ip`. It returns `404` when none is attached.

## Move it to another instance

Detach it, then attach it to the new instance:

```http
POST /api/v1/compute/floating-ips/{floatingIpId}/detach
```

Detaching removes the forwarding and keeps the address allocated to your tenant. It returns `409` if the floating IP was not attached.

## Change the reverse DNS name

`PATCH /api/v1/compute/floating-ips/{floatingIpId}` replaces `ptrTarget`. An empty string clears it and stops publishing a reverse DNS record for the address.

```sh
curl -X PATCH https://lahijan.example.com/api/v1/compute/floating-ips/$FLOATING_IP_ID \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{ "ptrTarget": "mail.example.com." }'
```

Make sure the forward DNS name also points at the address, for example with an `A` record in one of your [zones](/docs/dns/zones).

## List and release floating IPs

- `GET /api/v1/compute/floating-ips` lists the tenant's floating IPs, newest first (`limit`, `offset`).
- `GET /api/v1/compute/floating-ips/{floatingIpId}` returns one.
- `DELETE /api/v1/compute/floating-ips/{floatingIpId}` releases the address back to the pool. It detaches it first if needed and removes the reverse DNS record. Returns `204`.

> [!WARNING]
> Once released, the address can be handed to another tenant. You are not guaranteed to get the same address back.

## Permissions

| Action                                        | Permission                   | Default roles                |
| --------------------------------------------- | ---------------------------- | ---------------------------- |
| List and view                                 | `compute.floating_ip.read`   | Viewer, Member, Admin, Owner |
| Allocate, attach, detach, change PTR, release | `compute.floating_ip.manage` | Member, Admin, Owner         |
| Manage IP pools                               | `compute.ip_pool.manage`     | Platform administrators only |
