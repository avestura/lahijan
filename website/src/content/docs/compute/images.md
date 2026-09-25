---
title: Images
description: Browse the image catalog, add custom image entries and pick an image when you create an instance.
---

Every instance starts from an image: a ready-made operating system such as Ubuntu, Debian, Alpine or Fedora. This page explains where images come from, how the tenant image catalog works, and how to choose an image when you create an instance.

## Where images come from

An image is identified by an alias such as `ubuntu/24.04` or `alpine/3.22`. When you create an instance, the alias is resolved like this:

- An alias that contains a `/` (for example `debian/12`) is downloaded on demand from the public image server. The first instance from a new image takes longer to create while the image downloads.
- An alias without a `/` is looked up in the images already stored for your tenant.

You do not have to add an image to the catalog before using it. Any alias the public image server publishes works in the create wizard and the API.

Pick an image that is published for the instance type you want. Many images exist in both container and virtual machine variants, but not all of them.

## The image catalog

Go to **Instances** and click **Images** to open the catalog. It lists each image's **Alias**, **Source**, **Type**, **Arch** and **Fingerprint**.

There are two sources:

| Source       | Meaning                                                                                                                                                                                                           |
| ------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **featured** | Images the operator advertises to every tenant. A default installation features `ubuntu/24.04`, `debian/12`, `alpine/3.22` and `fedora/43`; your operator may change the list. Featured images cannot be deleted. |
| **custom**   | Entries someone in your tenant added.                                                                                                                                                                             |

The catalog only feeds the image picker in the create wizard. It does not limit which aliases you can use.

### Browse the public catalog

Click **Browse remote images** to search the public image catalog. Filter by **Operating system** and **Architecture**, or search by alias, OS or release, then click **Use** on an entry. The **Add image** dialog opens with the alias filled in.

Your browser loads this catalog directly from the public image server. If your network blocks that request, you see **Could not load the remote catalog.** You can still type an alias by hand.

### Add a custom image entry

1. On the **Images** page, click **Add image**.
2. Fill in **Alias** and **Fingerprint**. Both are required. **Browse the remote catalog to fill the alias** fills in the alias from the public catalog; you still enter the fingerprint yourself.
3. Optionally set **Type** (**Container** or **Virtual Machine**), **Architecture** (for example `amd64`) and **Description**.
4. Click **Add**.

> [!NOTE]
> Adding an entry only records it in your tenant's catalog. It does not upload image files. The alias must be one the system can already resolve, either from the public image server or from images already stored for your tenant.

With the API, `POST /api/v1/compute/images` does the same:

```sh
curl -X POST https://lahijan.example.com/api/v1/compute/images \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{
    "alias": "ubuntu/22.04",
    "fingerprint": "sha256:0f1e2d3c...",
    "type": "container",
    "architecture": "amd64",
    "description": "Ubuntu 22.04 LTS for legacy apps"
  }'
```

`alias` (1 to 63 characters) and `fingerprint` are required; `type` defaults to `container`. You can also pass `sizeBytes` and a `properties` map of strings. An alias that is already in the catalog returns `409`.

### Remove a custom image entry

Click the trash icon on a custom row, or call `DELETE /api/v1/compute/images/{imageId}`. Removing an entry does not affect instances already created from that image. Featured images cannot be removed.

### List images with the API

- `GET /api/v1/compute/images` returns a page of catalog entries (`limit`, `offset`).
- `GET /api/v1/compute/images/{imageId}` returns one entry.

## Use an image to create an instance

In the **New instance** wizard, the first step is **Image**. Choose an entry from the catalog list, or pick **Custom alias** and type any alias, such as `ubuntu/24.04`. **Browse** opens the public catalog in the same way as on the Images page.

In the API, pass the alias as `imageAlias`:

```json
{ "name": "db-1", "type": "virtual-machine", "imageAlias": "ubuntu/24.04" }
```

After creation, the instance's **Overview** tab shows the **Image** alias and, once resolved, the exact **Fingerprint** that was used. See [Instances](/docs/compute/instances) for the full create flow.

## Permissions

| Action           | Permission                | Default roles                |
| ---------------- | ------------------------- | ---------------------------- |
| View the catalog | `compute.image.read`      | Viewer, Member, Admin, Owner |
| Add an entry     | `compute.instance.create` | Member, Admin, Owner         |
| Remove an entry  | `compute.instance.delete` | Admin, Owner                 |
