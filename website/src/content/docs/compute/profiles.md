---
title: Profiles
description: Reuse configuration and devices across instances with profiles, including the default profile every tenant gets.
---

A profile is a named set of configuration keys and devices that you apply to instances. Instead of repeating the same CPU limit or the same data disk on every instance, you put it in a profile and list the profile on each instance. Values set on the instance itself override values from its profiles.

Profiles are managed through the API. In the dashboard you can pick a profile when creating an instance and see what each profile contributes on the instance page.

## The default profile

Every tenant has a profile named `default`. Lahijan sets it up the first time you create something in compute, and it contains:

- a `root` disk on the operator's storage pool, with no size limit, and
- on a default installation, an `eth0` network interface on the operator's shared network.

New instances use `default` unless you list other profiles. If you replace it with your own list, make sure one of the profiles still provides a root disk and a network interface, or add them as instance devices.

The `default` profile is not returned by the profile list below, which only shows profiles created in your tenant through Lahijan.

## Create a profile

`POST /api/v1/compute/profiles` creates a profile. Only `name` is required (1 to 63 characters, unique in the tenant).

```sh
curl -X POST https://lahijan.example.com/api/v1/compute/profiles \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "small-with-data",
    "description": "2 vCPU, 2 GiB, data volume on /srv",
    "config": { "limits.cpu": "2", "limits.memory": "2048MiB" },
    "devices": {
      "data": { "type": "disk", "pool": "default", "source": "app-data", "path": "/srv" }
    }
  }'
```

- `config` is a map of string keys to string values.
- `devices` is a map of device name to a map of device properties. Each device needs a `type`.
- The response is `201` with the profile. A name already in use returns `409`.

The same restrictions as for instance devices apply: GPU, USB, InfiniBand and raw `unix-char` or `unix-block` devices are refused, and NICs must use a managed network. See [Restrictions](/docs/compute/overview#restrictions).

> [!NOTE]
> CPU and memory limits that come from a profile do not count toward your [quota](/docs/compute/overview#quotas). Only values set on the instance itself are counted.

## List, view and delete profiles

- `GET /api/v1/compute/profiles` returns a page of profiles (`limit`, `offset`).
- `GET /api/v1/compute/profiles/{profileId}` returns one profile with its `config` and `devices`.
- `DELETE /api/v1/compute/profiles/{profileId}` deletes it and returns `204`.

There is no update call. To change a profile, create a new one and switch your instances to it.

> [!WARNING]
> Delete a profile only when no instance uses it anymore. Instances still listing it depend on its devices and settings.

## Apply profiles to an instance

When you create an instance in the dashboard, open **Advanced options** on the **Size** step and pick a **Profile**. The dashboard applies exactly one profile.

With the API you can apply several by listing them in `profiles`. The instance's own configuration and devices still override anything that comes from a profile:

```json
{
	"name": "app-1",
	"imageAlias": "debian/12",
	"profiles": ["default", "small-with-data"]
}
```

To change the profiles of an existing instance, send the full new list:

```sh
curl -X PATCH https://lahijan.example.com/api/v1/compute/instances/$INSTANCE_ID \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{ "profiles": ["default", "small-with-data"] }'
```

## See what a profile contributes

On the instance page:

- **Overview** lists the instance's **Profiles**.
- **Config** > **Effective configuration** shows every key in force; the **Source** column marks keys that come from a **profile**.
- **Network** and **Storage** list devices inherited from profiles alongside the instance's own devices.

## Permissions

| Action                        | Permission                | Default roles                |
| ----------------------------- | ------------------------- | ---------------------------- |
| List and view                 | `compute.profile.read`    | Viewer, Member, Admin, Owner |
| Create and delete             | `compute.profile.apply`   | Member, Admin, Owner         |
| Change an instance's profiles | `compute.instance.update` | Member, Admin, Owner         |
