# ADR-0015: REST + OpenAPI as source of truth

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** maintainer

## Context

Lahijan needs a public API. The API surface also drives the frontend (typed
client) and any third-party integrations.

Options considered:

- **REST only (spec later)** — fast to ship; weak contract; frontend types
  drift from reality.
- **REST + OpenAPI 3.1 spec as source of truth** — clients (Go + TS)
  generated; clear contract; spec-first or code-first generation.
- **REST + GraphQL** — flexible queries for power users; two APIs to maintain.
- **gRPC + REST gateway** — typed internal core; REST for public; complex
  build; overkill for a monolith.

## Decision

Lahijan's public API is **REST**, and the **OpenAPI 3.1 specification is the
source of truth**. From the spec we generate:

- The Go server types (via `oapi-codegen`) used by Fiber handlers.
- The TypeScript client (via `openapi-typescript` + `openapi-fetch`) used by
  both `web/` and `website/`.
- A future Go client SDK (under `pkg/lahijan-client/`) for third-party Go code.

Spec lives at `api/openapi.yaml`. CI regenerates clients and fails if the spec
and generated code drift.

## Consequences

- **Positive:** one source of truth; types can't drift from spec.
- **Positive:** frontend gets a typed client for free.
- **Positive:** external contributors can use any OpenAPI-compatible tool.
- **Negative:** every endpoint change requires editing the spec.
- **Negative:** oapi-codegen adds a small codegen step to the build.

## Compliance

- `api/openapi.yaml` is the canonical spec.
- `api/gen/go/` and `api/gen/ts/` hold generated code.
- Fiber handlers use generated types for request/response.
- Frontend uses `lib/api/client` which wraps `openapi-fetch` against the spec.
- All public routes are under `/api/v1/`.

## References

- [OpenAPI 3.1 spec](https://spec.openapis.org/oas/v3.1.0)
- [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen)
- [openapi-typescript](https://github.com/drwpow/openapi-typescript)
- ADR-0014 (frontend stack)
- WS-05 (REST API framework + OpenAPI pipeline)
