---
title: Access tokens
description: Create, use and revoke personal access tokens for scripts and command-line tools that call the Lahijan API.
---

A personal access token lets a script, CI job or command-line tool call the Lahijan API as you, without a browser session. You create tokens on **Settings > Access Tokens** or through the API, and you can revoke them at any time.

## How tokens work

- A token acts as your user. Each request it makes is checked against the role you hold in the tenant you address, exactly like a request from the dashboard.
- The raw token is shown once, when you create it. Lahijan stores only a hash, so a lost token cannot be recovered; create a new one.
- Tokens start with the prefix `lah_pat_` (your operator can change it with `auth.pat.prefix`), which makes them easy to spot in logs and secret scanners.
- Each token records when it was last used, and every use is written to the audit log as `auth.pat.use`.

> [!WARNING]
> Scopes are recorded on the token but are not enforced in the current release. A token can do everything your role allows in any tenant you belong to, including creating more tokens. Treat every token like your password, give it an expiry, and revoke it when you no longer need it.

## Create a token in the dashboard

1. Open **Settings > Access Tokens**.
2. Select **New token**.
3. Enter a **Name** that says where the token is used, for example `ci-deploy`.
4. Optionally enter **Scopes** as comma-separated permission slugs, for example `compute.instance.read, compute.instance.start`. See [Permissions](/docs/reference/permissions) for the slugs.
5. Select **Create**.
6. In the **Token created** dialog, select **Copy token**, store it somewhere safe, then select **I've saved it**.

The dashboard form does not set an expiry, so tokens created there never expire. Use the API if you want an expiry date.

The token list shows **Name**, **Scopes**, **Last used** and **Expires** ("Never" when there is no expiry).

## Create a token with the API

`POST /api/v1/auth/personal-access-tokens` creates a token for the signed-in user. Only `name` is required.

```sh
curl -X POST https://cloud.example.com/api/v1/auth/personal-access-tokens \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
        "name": "nightly-backup",
        "scopes": ["compute.snapshot.create"],
        "expiresAt": "2026-12-31T23:59:59Z"
      }'
```

| Field       | Required | Meaning                                                                                           |
| ----------- | -------- | ------------------------------------------------------------------------------------------------- |
| `name`      | Yes      | A label for the token. Must not be empty.                                                         |
| `scopes`    | No       | Permission slugs. Each must contain a dot (`scope.action`); anything else is rejected with `400`. |
| `expiresAt` | No       | RFC 3339 timestamp. After this time the token stops working. Omit or send `null` for no expiry.   |

The `201` response includes the raw value in `token`. It is the only response that ever contains it.

```json
{
	"id": "6f1c1a8e-1f7a-4c1e-9a53-2b4a0f0b8e21",
	"name": "nightly-backup",
	"scopes": ["compute.snapshot.create"],
	"token": "lah_pat_...",
	"expiresAt": "2026-12-31T23:59:59Z",
	"lastUsedAt": null,
	"createdAt": "2026-09-25T10:00:00Z"
}
```

`GET /api/v1/auth/personal-access-tokens` lists your tokens that are not revoked, newest first, without the raw values.

## Use a token

Send the token in the `Authorization` header with the `Bearer` scheme:

```http
Authorization: Bearer lah_pat_...
```

For example, to read your own account:

```sh
curl https://cloud.example.com/api/v1/auth/me \
  -H "Authorization: Bearer $LAHIJAN_TOKEN"
```

### Tenant header

Most endpoints act inside a tenant (compute, DNS, object storage, billing, audit, agent). Tell Lahijan which tenant with one of these headers:

| Header          | Value                                                                          |
| --------------- | ------------------------------------------------------------------------------ |
| `X-Tenant-Id`   | The tenant's UUID. Find it in the `memberships` list of `GET /api/v1/auth/me`. |
| `X-Tenant-Slug` | The tenant's slug, if you know it.                                             |

```sh
curl https://cloud.example.com/api/v1/compute/instances \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $LAHIJAN_TENANT_ID"
```

Without a tenant header, tenant-scoped endpoints answer `400` with the code `tenant_scope_required`. With a tenant you do not belong to, or an action your role does not allow, they answer `403` with the code `forbidden`; `error.details.permission` names the permission that was missing.

An invalid, expired or revoked token is ignored, so the request is treated as anonymous and protected endpoints answer `401`.

## Revoke a token

In the dashboard, select **Revoke** next to the token. With the API:

```sh
curl -X DELETE https://cloud.example.com/api/v1/auth/personal-access-tokens/6f1c1a8e-1f7a-4c1e-9a53-2b4a0f0b8e21 \
  -H "Authorization: Bearer $LAHIJAN_TOKEN"
```

Revocation takes effect on the next request that uses the token. Revoked tokens are kept for the audit trail but no longer appear in your list. You can only revoke your own tokens; any other id returns `404`.

## Good practice

- Create one token per script or machine, so you can revoke one without breaking the others.
- Set `expiresAt` for anything that is not meant to live forever.
- Do not commit tokens to source control. Pass them through your CI system's secret store or an environment variable.
- Check **Last used** from time to time and revoke tokens that have not been used in a long while.
