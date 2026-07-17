# `/api`

The OpenAPI 3.1 specification that is the **single source of truth** for
Lahijan's public HTTP API (see [ADR-0015](../docs/adr/0015-rest-openapi.md)).

## Layout

```
api/
  openapi.yaml                 # THE spec. Edit this, never the generated code.
  oapi-codegen.server.yaml     # oapi-codegen config -> api/gen/go/ (Fiber server + types)
  oapi-codegen.client.yaml     # oapi-codegen config -> pkg/lahijan-client/ (Go client SDK)
  package.json                 # TS codegen toolchain (openapi-typescript + openapi-fetch)
  gen/
    go/                        # generated Go server types + Fiber interface (DO NOT EDIT)
    ts/                        # generated TypeScript schema types       (DO NOT EDIT)
    ts-client/                 # hand-written openapi-fetch client wrapper consuming gen/ts
```

## Regenerating

```bash
make openapi-gen      # regenerate Go server + Go client + TS schema
make openapi-verify   # CI: fail if committed generated code has drifted
```

Generated code under `api/gen/` and `pkg/lahijan-client/` is committed so
builds don't require the codegen tools at compile time. CI (`OpenAPI` workflow)
fails on drift.

## Rules

- All public routes live under `/api/v1/`.
- Every error response uses the standard `Error` envelope
  `{error: {code, message, details?}}`.
- Backend infrastructure names (Incus, PowerDNS, SeaweedFS) never appear here;
  use the user-facing terms (compute / dns / object storage).
