---
title: REST API
description: Conventions of the Lahijan REST API, including authentication, tenant scoping, errors, pagination, CORS, health checks, the OpenAPI spec and generated clients.
---

Everything the dashboard does goes through the Lahijan REST API, so you can script any of it. This page covers the rules shared by every endpoint. The complete, typed contract is the OpenAPI 3.1 spec in the repository at `api/openapi.yaml`.

## Base URL and format

- All endpoints live under `/api/v1/`, on the same origin as the dashboard (for example `https://lahijan.example.com/api/v1/`). The exceptions are the health endpoints and the job queue UI.
- Request and response bodies are JSON unless an endpoint says otherwise (plugin upload is multipart, receipt downloads are PDF).
- JSON field names are camelCase (`tenantId`, `createdAt`).
- Timestamps are RFC 3339 strings in UTC. Ids are UUIDs, except job ids, which are integers.
- Send `Accept-Language: fa` to get error messages in Persian; English is the default.
- Every response carries an `X-Request-ID` header. Include it when you report a problem; it ties the request to logs and audit entries.

## Authentication

There are two ways to authenticate. Unauthenticated requests are not rejected up front: they reach the endpoint as anonymous, and endpoints that need a user answer `401 unauthorized`.

### Session cookie (dashboard)

Signing in through `POST /api/v1/auth/login` sets a session cookie, `lahijan_session` by default, and a refresh cookie, `lahijan_refresh`. The browser sends them automatically. `POST /api/v1/auth/refresh` renews the session and `POST /api/v1/auth/logout` ends it. The cookie names come from `auth.session.cookieName` and `auth.session.refreshCookieName`.

### Personal access token (scripts)

For scripts and tools, create a personal access token (see [Access tokens](/docs/account/access-tokens)) and send it as a bearer token:

```http
GET /api/v1/compute/instances HTTP/1.1
Host: lahijan.example.com
Authorization: Bearer lah_pat_...
X-Tenant-Id: 6f1c2b7e-8a4d-4f7e-9c1a-2d3b4e5f6a7b
```

Send the token exactly as shown when it was created, including the `lah_pat_` prefix. Revoked and expired tokens are treated as anonymous. Each use updates the token's last-used time and writes an audit entry. If a request carries both a valid session cookie and a token, the cookie wins.

## Tenant scope

Most endpoints act inside one tenant. Tell Lahijan which one with a header:

| Form                             | Example                                                                                    |
| -------------------------------- | ------------------------------------------------------------------------------------------ |
| `X-Tenant-Id` header (preferred) | `X-Tenant-Id: 6f1c2b7e-...`                                                                |
| `tenant_id` query parameter      | `?tenant_id=6f1c2b7e-...`, for clients that cannot set headers, such as browser WebSockets |
| `X-Tenant-Slug` header           | `X-Tenant-Slug: acme`, resolved to the tenant id                                           |

They are tried in that order. The value is only a hint: every protected endpoint checks that you hold the needed permission in that tenant. A protected endpoint called without a usable tenant returns `400` with the code `tenant_scope_required`. Admin endpoints under `/api/v1/admin/` need the header too.

## Permissions

Each protected endpoint requires one permission, such as `compute.instance.create` or `dns.record.delete`. Without it you get `403 forbidden`, and `details.permission` names the missing permission. The full list is in [Permissions](/docs/reference/permissions).

## Errors

Every error uses the same envelope:

```json
{
	"error": {
		"code": "forbidden",
		"message": "You do not have permission to do that.",
		"details": { "permission": "dns.zone.delete" }
	}
}
```

- `code` is a stable, machine-readable string. Switch on it.
- `message` is safe to show to users and is localized.
- `details` is optional and free-form; its shape depends on the error.

Error codes in use:

| Code                    | HTTP status | When                                                                                              |
| ----------------------- | ----------- | ------------------------------------------------------------------------------------------------- |
| `bad_request`           | 400         | Malformed input or failed validation.                                                             |
| `tenant_scope_required` | 400         | A protected endpoint was called without a tenant scope.                                           |
| `unauthorized`          | 401         | Not signed in, or the credential is invalid.                                                      |
| `payment_required`      | 402         | The action would take the balance below the allowed floor.                                        |
| `insufficient_balance`  | 402         | Creating an instance without enough balance.                                                      |
| `forbidden`             | 403         | Missing permission.                                                                               |
| `not_found`             | 404         | The resource does not exist, or the route does not exist.                                         |
| `conflict`              | 409         | Duplicate or state conflict (for example retrying a job that is not discarded).                   |
| `payload_too_large`     | 413         | The body or an uploaded file is over its limit.                                                   |
| `quota_exceeded`        | 422         | A compute quota would be exceeded. `details` has `dimension`, `limit`, `current` and `requested`. |
| `too_many_requests`     | 429         | Too many sign-in verification attempts.                                                           |
| `rate_limited`          | 429         | The agent's rate limit or spend cap was reached.                                                  |
| `internal`              | 500         | Unexpected server error. The cause is logged, not returned.                                       |
| `not_implemented`       | 501         | The feature is turned off on this server (for example plugins or jobs disabled).                  |
| `service_unavailable`   | 503         | A backend could not serve the request.                                                            |

A few endpoints return other statuses with a standard code, such as `422` with `bad_request` when a marketplace hash does not match.

## Pagination and filtering

List endpoints use offset pagination:

| Parameter | Meaning                                                      |
| --------- | ------------------------------------------------------------ |
| `limit`   | Items per page. Most endpoints default to 50 and cap at 200. |
| `offset`  | Items to skip. Default 0.                                    |

Responses wrap the items with the paging values:

```json
{ "items": [ ... ], "total": 132, "limit": 50, "offset": 0 }
```

Some lists differ. Object version listings use S3-style markers (`nextKeyMarker`, `nextVersionIdMarker`, `isTruncated`). The admin jobs list does not apply `offset` (see [Background jobs](/docs/admin/jobs#inspect-jobs-with-the-api)). Filters are plain query parameters specific to each endpoint, for example `action`, `resourceType`, `fromTs` and `toTs` on the audit log, or `state`, `kind` and `queue` on jobs.

## Idempotency and rate limits

- Lahijan does not support an `Idempotency-Key` header. Retrying a `POST` can create a second resource; check before you retry.
- There is no general API rate limit. Only specific flows return `429`, as listed above. Put a limit in your reverse proxy if you need one.

## CORS

CORS is off by default because the dashboard is served from the same origin as the API. To call the API from a browser app on another origin, set the `http.server.cors` keys:

```yaml
http:
  server:
    cors:
      enabled: true
      allowOrigins: ["https://app.example.com"]
      allowMethods: ["GET", "POST", "PATCH", "DELETE"]
      allowHeaders: ["Authorization", "Content-Type", "X-Tenant-Id"]
      maxAge: 600
```

## Request size

The server's body limit is `http.server.bodylimit`, 4 MiB by default. Larger requests get `413 payload_too_large`. Endpoints can apply smaller limits of their own.

## Health checks

| Endpoint                     | Returns                                                  | Configured by                                          |
| ---------------------------- | -------------------------------------------------------- | ------------------------------------------------------ |
| `GET /health`                | `{"status": "ok", "version": "..."}`. No authentication. | Always on.                                             |
| `GET /healthcheck/liveness`  | `200` while the process is up.                           | `http.server.healthcheck.enabled`, `livenessEndpoint`  |
| `GET /healthcheck/readiness` | `200` while the process is up.                           | `http.server.healthcheck.enabled`, `readinessEndpoint` |
| `GET /api/v1/ping`           | A pong with the server time. No authentication.          | Always on.                                             |

The liveness and readiness probes do not check the database or the backends; they only show the process is serving HTTP.

## OpenAPI spec and clients

`api/openapi.yaml` is the single source of truth. Code generated from it is committed to the repository:

| Path                  | What it is                                                                                                        |
| --------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `api/gen/go/`         | Go server types and the routing interface used by Lahijan itself.                                                 |
| `pkg/lahijan-client/` | Go client for the REST API, generated with oapi-codegen. Import `github.com/avestura/lahijan/pkg/lahijan-client`. |
| `api/gen/ts/`         | TypeScript types for every schema and path.                                                                       |
| `api/gen/ts-client/`  | A small TypeScript client built on openapi-fetch and those types.                                                 |

Regenerate with `make openapi-gen`. `sdk-go/` is a different thing: the SDK for writing [plugins](/docs/plugins/overview), not a REST client.

```go
client, err := lahijanclient.NewClient("https://lahijan.example.com")
if err != nil {
	return err
}
resp, err := client.PingWithResponse(ctx)
```

## Endpoint groups

Paths from the OpenAPI spec, grouped by area. Braces mark path parameters.

| Area                                      | Paths                                                                                                                                                                                                                                                    |
| ----------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Platform                                  | `/health`, `/api/v1/ping`                                                                                                                                                                                                                                |
| Sign-in and account                       | `/api/v1/auth/register`, `login`, `logout`, `refresh`, `verify-email`, `resend-verification`, `password-reset/request`, `password-reset/confirm`, `me`                                                                                                   |
| Access tokens                             | `/api/v1/auth/personal-access-tokens`, `/api/v1/auth/personal-access-tokens/{tokenId}`                                                                                                                                                                   |
| External sign-in                          | `/api/v1/auth/oauth/{provider}/start` and `callback`, `/api/v1/auth/oidc/{provider}/start` and `callback`, `/api/v1/auth/saml/metadata`, `/api/v1/auth/saml/{provider}/start` and `acs`                                                                  |
| Linked identities                         | `/api/v1/me/identities`, `/api/v1/me/identities/{identityId}`                                                                                                                                                                                            |
| Multi-factor                              | `/api/v1/me/mfa/totp/enroll`, `verify`, `disable`; `/api/v1/me/mfa/webauthn/register/begin` and `finish`, `login/begin` and `finish`, `credentials/{credentialId}`; `/api/v1/me/mfa/recovery`; `/api/v1/auth/mfa/challenge`                              |
| Audit log                                 | `/api/v1/audit`, `/api/v1/audit/{auditId}`, `/api/v1/audit/export`                                                                                                                                                                                       |
| Instances                                 | `/api/v1/compute/instances`, `/{instanceId}`, `/{instanceId}/{action}`, `exec`, `vnc`, `console`, `runtime`, `logs`, `logs/{logFile}`, `migrate`, `floating-ip`                                                                                          |
| Images, profiles, networks, storage pools | `/api/v1/compute/images`, `/api/v1/compute/profiles`, `/api/v1/compute/networks`, `/api/v1/compute/storage`, each with a `/{id}` path                                                                                                                    |
| Snapshots and backups                     | `/api/v1/compute/instances/{instanceId}/snapshots`, `/snapshots/{snapshotId}`, `/snapshots/{snapshotId}/restore`, `/backups`; `/api/v1/compute/snapshot-policies`, `/api/v1/compute/backup-targets`, `/api/v1/compute/backups`, each with a `/{id}` path |
| Compute cluster                           | `/api/v1/compute/cluster/members`, `/{memberName}`, `/{memberName}/{action}`                                                                                                                                                                             |
| Floating IPs                              | `/api/v1/compute/floating-ips`, `/{floatingIpId}`, `/{floatingIpId}/attach`, `/{floatingIpId}/detach`                                                                                                                                                    |
| IP pools (admin)                          | `/api/v1/admin/compute/ip-pools`, `/{poolId}`, `/{poolId}/ranges`, `/{poolId}/ranges/{rangeId}`                                                                                                                                                          |
| DNS zones and records                     | `/api/v1/dns/zones`, `/{zoneId}`, `/{zoneId}/records`, `/{zoneId}/records/{recordId}`, `/{zoneId}/dnssec/{action}`, `/{zoneId}/apply-template`; `/api/v1/dns/templates`                                                                                  |
| Domains                                   | `/api/v1/dns/domains`, `/{domainId}`, `/{domainId}/renew`, `/search`, `/transfer`                                                                                                                                                                        |
| Buckets                                   | `/api/v1/storage/buckets`, `/{bucketId}`, `credentials`, `credentials/{credentialId}`, `presign`, `quota`, `usage`                                                                                                                                       |
| Versioning, lifecycle, object lock        | `/api/v1/storage/buckets/{bucketId}/versioning`, `versions`, `versions/restore`, `lifecycle`, `lifecycle/{ruleId}`, `lifecycle/{ruleId}/status`, `object-lock`                                                                                           |
| Balance and receipts                      | `/api/v1/me/balance`, `usage`, `ledger`, `receipts`, `receipts/{receiptId}`, `receipts/{receiptId}.pdf`                                                                                                                                                  |
| Payments and plans                        | `/api/v1/billing/config`, `payment-methods`, `payment-methods/{paymentMethodId}`, `topup`, `subscriptions`, `subscriptions/{subscriptionId}`, `redeem`, `plans`; `/api/v1/webhooks/stripe`                                                               |
| Billing administration                    | `/api/v1/admin/billing/prices`, `plans`, `plans/{planId}`, `plans/{planId}/push`, `promo-codes`, `promo-codes/{promoCodeId}/revoke`, `webhook-events`; `/api/v1/admin/users/{userId}/topup`, `refund`, `ledger`, `balance`                               |
| Agent                                     | `/api/v1/agent/conversations`, `/{conversationId}`, `/{conversationId}/messages`; `/api/v1/agent/tool-calls/{toolCallId}/confirm`; `/api/v1/agent/providers`, `/{providerId}`; `/api/v1/agent/policy`                                                    |
| Background jobs (admin)                   | `/api/v1/admin/jobs`, `/{jobId}`, `/{jobId}/retry`, `/{jobId}/cancel`                                                                                                                                                                                    |
| Plugins (admin)                           | `/api/v1/admin/plugins`, `upload`, `/{pluginId}`, `/{pluginId}/permissions/{permission}/{action}`, `/{pluginId}/enable`, `/{pluginId}/disable`, `install/{name}`, `upgrade/{name}`                                                                       |
| Marketplace (admin)                       | `/api/v1/admin/marketplace`, `/api/v1/admin/marketplace/{name}`                                                                                                                                                                                          |

Paths in a row that do not start with `/` are relative to the first path in that row.
