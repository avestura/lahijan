---
title: Networks
description: How instances get network access, how to inspect interfaces, and how to create and attach your own networks through the API.
---

Every instance needs a network interface to reach other instances and the internet. This page explains the network your instances get by default, where to see their addresses, and how to manage networks of your own through the API.

## The default network

On a default installation, each tenant's `default` profile gives every instance an `eth0` interface on a shared network that the operator set up. You do not need to create anything: start an instance and it gets an address.

Your operator can instead enable per-tenant networks, where each tenant has its own isolated network space and creates its own networks. Whether this is available depends on the deployment. Ask your operator before you plan around custom networks.

## See an instance's network

Open the instance and select the **Network** tab.

- **Interfaces** shows the live network state reported by the running instance: each interface's **State**, **Addresses**, **MAC**, **MTU**, and **Received** and **Sent** traffic. It is empty while the instance is stopped.
- **NIC devices** lists the network devices in force, including ones inherited from profiles, with the network each one is **Attached to**.

The **Overview** tab also lists the instance's routable **IP addresses** in one place.

For scripts, `GET /api/v1/compute/instances/{instanceId}/runtime` returns the same data. The `networks` field maps each interface name to its addresses and counters.

## Create a network

Networks are managed through the API only. `POST /api/v1/compute/networks` creates one in your tenant. Only `name` is required (1 to 63 characters, unique in the tenant); `type` defaults to `bridge`.

```sh
curl -X POST https://lahijan.example.com/api/v1/compute/networks \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "backend",
    "description": "Private network for the app tier",
    "type": "bridge",
    "config": {}
  }'
```

`config` is a map of string network settings passed to the compute system as is. The response is `201` with the network object. A name already in use returns `409`. If the compute system refuses the network (for example, because the deployment does not allow tenant networks), the request fails and nothing is saved.

## List, view and delete networks

- `GET /api/v1/compute/networks` returns a page of networks (`limit`, `offset`). It lists networks created in your tenant through Lahijan; the operator's shared network is not included.
- `GET /api/v1/compute/networks/{networkId}` returns one network with its `type`, `config` and any `aclNames` and `forwardNames`.
- `DELETE /api/v1/compute/networks/{networkId}` deletes it and returns `204`.

There is no update call. Detach every instance from a network before you delete it.

## Attach an instance to a network

Add a `nic` device that names the network. In the dashboard, open the instance's **Config** tab and, under **Instance devices**, add a device:

- **Name**: for example `eth1`
- **Type**: `nic`
- **Properties**:

  ```ini
  network=backend
  ```

With the API, send the device in a `PATCH`. The `devices` map replaces all of the instance's own devices, so include any you already have:

```sh
curl -X PATCH https://lahijan.example.com/api/v1/compute/instances/$INSTANCE_ID \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{ "devices": { "eth1": { "type": "nic", "network": "backend" } } }'
```

NIC devices must point at a managed network with `network=...`. Other NIC kinds are refused by the server.

To give several instances the same network setup, put the `nic` device in a [profile](/docs/compute/profiles).

## Public addresses

To reach an instance from the internet on a fixed public address, allocate a [floating IP](/docs/compute/floating-ips) and attach it to the instance.

## Permissions

| Action                      | Permission                | Default roles                |
| --------------------------- | ------------------------- | ---------------------------- |
| List and view networks      | `compute.network.read`    | Viewer, Member, Admin, Owner |
| Create and delete networks  | `compute.network.create`  | Admin, Owner                 |
| Attach a NIC to an instance | `compute.instance.update` | Member, Admin, Owner         |
