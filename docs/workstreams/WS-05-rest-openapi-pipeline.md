# WS-05 · REST API Framework + OpenAPI Pipeline

```
Status: pending
Phase: 0
Depends on: WS-04
Unblocks: WS-06, WS-08, WS-11..13, every API-touching WS
```

## Goal

Establish the HTTP API foundation: error envelope, request validation,
middleware stack, versioning, the OpenAPI spec as source of truth, and codegen
for both server types and clients (Go + TypeScript). After this WS, every
domain module just plugs in routes.

## Scope

**In scope:**
- `api/openapi.yaml` (OpenAPI 3.1 spec, structured by area; seeded with
  `/health` and a sample `/api/v1/me` for the future auth module).
- `api/gen/go/` (oapi-codegen output: server interfaces + types).
- `api/gen/ts/` (openapi-typescript output: schema types).
- `api/gen/ts-client/` (openapi-fetch-based client).
- `internal/app/lahijan/api/` package:
  - `server.go` (Fiber server using oapi-codegen's `fiber.go` server)
  - `errors.go` (standard error envelope helper `SendError(c, code, msg,
    details)`)
  - `validate.go` (request body validation against the spec)
  - `router.go` (route registration, grouped per area)
  - `middleware/{request_id,tenant,auth,rbac,audit,recover,cors,logger}.go`
- A CI step that regenerates clients and fails if the spec drifts from
  generated code.
- A "hello world" `/api/v1/ping` endpoint to prove the pipeline works.

**Out of scope:**
- Auth/RBAC/audit logic (WS-06, WS-08) — but the middleware **slots** are
  defined here.
- Real domain endpoints (Phase 3+).
- The TS client's actual usage in the frontend (WS-18).

## Required reading for the AI session

- `/AGENTS.md`
- `internal/app/lahijan/AGENTS.md`
- `docs/adr/0015-rest-openapi.md`
- `docs/architecture/conventions.md#http-api`
- `.opencode/skills/backend-foundations/SKILL.md`

## Deliverables

- `api/openapi.yaml` with at least `/health` and `/api/v1/ping`.
- Generated Go server code + Go client SDK under `pkg/lahijan-client/`.
- Generated TS schema + client.
- Standard error envelope implemented and tested.
- Middleware skeleton (real auth/rbac/audit come later; here we define the
  shape and pass-through).
- CI step: regenerate + diff check.

## Definition of Done

- [ ] `api/openapi.yaml` validates against OpenAPI 3.1
- [ ] `make openapi-gen` (new target) regenerates Go + TS code; no diff
- [ ] `/api/v1/ping` returns `{"pong":"<timestamp>"}` and emits a trace
- [ ] error envelope helper produces the exact `{error:{code,message,details}}` shape
- [ ] every error response uses the envelope
- [ ] middleware slots defined and ordered per conventions
- [ ] CI fails if the spec and generated code drift
- [ ] `make lint test` green

## Open questions

- oapi-codegen vs. fiber's native + hand-written types? (Default: oapi-codegen
  for type fidelity; Fiber for routing.)
- Do we ship a Go client SDK in `pkg/lahijan-client/` now or stub it?
  (Default: ship a real, minimal one.)

## Notes

- This WS will introduce several new deps: oapi-codegen (build-time),
  openapi-typescript (build-time), openapi-fetch (frontend runtime).
- The middleware slots defined here become the contract every later WS
  depends on. Get them right.
