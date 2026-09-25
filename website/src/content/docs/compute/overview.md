---
title: Compute overview
description: What the compute service offers, where it lives in the dashboard, which limits apply and which permissions you need.
---

Compute lets you run system containers and virtual machines inside your tenant. You create an instance from an image, give it CPU and memory, start it, and then work with it through a browser console, snapshots and its configuration. Everything is available from the dashboard and from the REST API under `/api/v1/compute/`.

## Instance types

Every instance has one of two types. You pick the type when you create the instance and cannot change it later.

| Type            | API value         | What it is                                                                                                                                                                        |
| --------------- | ----------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Container       | `container`       | A system container. It runs a full Linux userspace (init, services, package manager) and shares the host kernel. Starts in seconds and uses little overhead. This is the default. |
| Virtual machine | `virtual-machine` | A full virtual machine with its own kernel. Use it when you need a different kernel, kernel modules, or a graphical console.                                                      |

Both types support the interactive shell console, snapshots, configuration edits and the logs view. Only virtual machines get the graphical console. See [Console and exec](/docs/compute/console).

## Compute in the dashboard

The compute pages sit under **Instances** in the sidebar.

- **Instances** lists every instance in the current tenant, with a text filter, a status filter and bulk actions. The list refreshes every 5 seconds.
- **New instance** opens a three-step wizard (Image, Size, Review). See [Instances](/docs/compute/instances).
- **Images** (button at the top of the list) shows the tenant's image catalog. See [Images](/docs/compute/images).

Clicking an instance opens its detail page. The header shows the name, the status and the lifecycle buttons. Below it are these tabs:

| Tab                     | What it shows                                                                                                       |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------- |
| **Overview**            | IP addresses, memory, CPU time, processes, architecture, image, fingerprint, profiles, created and last used times. |
| **Console**             | An interactive shell inside the running instance.                                                                   |
| **Console (Graphical)** | The screen of a running virtual machine. Virtual machines only.                                                     |
| **Snapshots**           | Create, restore and delete snapshots.                                                                               |
| **Network**             | Live interfaces with addresses and traffic counters, plus the NIC devices in force.                                 |
| **Storage**             | Disks in force: the root disk plus any attached volumes, with size limits and usage.                                |
| **Config**              | The effective configuration, the instance's own settings and its own devices. Editable.                             |
| **Logs**                | The console output buffer and the runtime log files.                                                                |
| **Audit**               | Audit events recorded for this instance.                                                                            |

The Overview, Network, Storage and Config tabs read the live state of the instance. Usage figures and interface addresses are only reported while the instance is running.

Other compute features have no dashboard page yet and are available through the API only: [profiles](/docs/compute/profiles), [networks](/docs/compute/networks), [storage volumes](/docs/compute/storage), [floating IPs](/docs/compute/floating-ips), snapshot policies and backups (see [Snapshots and backups](/docs/compute/snapshots-and-backups)).

> [!NOTE]
> If the dashboard shows **This feature is not enabled** on the Instances page, the operator has not turned on compute for this deployment. The API returns `501` with the code `not_implemented` in that case.

## Quotas

Each tenant has a fixed compute quota. Lahijan checks it when you create an instance, before anything is provisioned.

| Dimension | Limit per tenant   | Counted from                                                                       |
| --------- | ------------------ | ---------------------------------------------------------------------------------- |
| Instances | 10                 | Every instance that has not been deleted.                                          |
| vCPUs     | 40                 | The `limits.cpu` value set on each instance (an instance without one counts as 1). |
| Memory    | 80 GiB (81920 MiB) | The `limits.memory` value set on each instance.                                    |
| Disk      | 800 GiB            | The `size` of a `root` disk device set on the instance itself.                     |

A create request that would go over a limit fails with `422` and the error code `quota_exceeded`. The error `details` tell you which limit was hit:

```json
{
	"error": {
		"code": "quota_exceeded",
		"message": "...",
		"details": {
			"dimension": "vcpu",
			"limit": 40,
			"current": 38,
			"requested": 42
		}
	}
}
```

`dimension` is one of `instances`, `vcpu`, `memory_mib` or `disk_gib`. Quotas are only checked at create time; editing an instance's configuration later does not re-check them.

## Restrictions

Your instances run in an isolated space per tenant. To keep tenants apart, some device types and settings are refused by the server even though the dashboard lets you enter them:

- GPU, USB, InfiniBand, `unix-char` and `unix-block` devices are blocked.
- NIC devices must attach to a managed network.
- Low-level container and virtual machine settings are blocked.
- Disk devices, snapshots and backups are allowed.

When a change is refused, the dashboard shows **Update rejected** with the reason.

## Permissions

Compute actions are controlled by permissions that come from your role in the tenant. The table lists the default roles; your tenant admin can also create custom roles.

| Action                                 | Permission                                    | Viewer | Member | Admin and owner |
| -------------------------------------- | --------------------------------------------- | :----: | :----: | :-------------: |
| View instances, logs, live state       | `compute.instance.read`                       |  yes   |  yes   |       yes       |
| Create an instance                     | `compute.instance.create`                     |   no   |  yes   |       yes       |
| Start, stop, restart, freeze, unfreeze | `compute.instance.start`, `.stop`, `.restart` |   no   |  yes   |       yes       |
| Edit configuration and devices         | `compute.instance.update`                     |   no   |  yes   |       yes       |
| Delete an instance                     | `compute.instance.delete`                     |   no   |   no   |       yes       |
| Interactive shell console              | `compute.instance.console.exec`               |   no   |  yes   |       yes       |
| Graphical console                      | `compute.instance.console.vnc`                |   no   |  yes   |       yes       |
| Take snapshots                         | `compute.snapshot.create`                     |   no   |  yes   |       yes       |
| Delete snapshots                       | `compute.snapshot.delete`                     |   no   |   no   |       yes       |

Snapshot policies, backup targets, networks, volumes and floating IPs have their own permissions, listed on each page. The full catalog is in [Permissions](/docs/reference/permissions).

## Next steps

- [Create your first instance](/docs/compute/instances)
- [Open a shell in it](/docs/compute/console)
- [Take a snapshot](/docs/compute/snapshots-and-backups)
