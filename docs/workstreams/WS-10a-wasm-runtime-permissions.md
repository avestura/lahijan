# WS-10a · WASM Runtime + Permission System

```
Status: done
Phase: 2
Depends on: WS-05, WS-08
Unblocks: WS-10b, WS-10c
```

## Goal

Stand up the WASM sandbox that will host untrusted plugins, and the
Android-style permission system that gates what a plugin can do. After this
WS, an admin can upload a `.wasm` module + a manifest, approve its
permissions, and the runtime can load and execute it (with no host functions
yet — those are WS-10b).

## Scope

**In scope:**
- `internal/app/lahijan/wasm/runtime.go`:
  - wazero host runtime (pure Go, no CGO)
  - module compilation + instantiation
  - per-plugin memory limit (configurable via `conf.wasm.max_memory_per_plugin`)
  - per-call execution timeout (conf.wasm.exec_timeout)
  - plugin instance pooling (compile once, instantiate per request)
- `internal/app/lahijan/wasm/permission/`:
  - permission model: `scope.action` (e.g. `network.outbound`,
    `kv.read:cache`, `kv.write:cache`, `events.emit`, `events.listen:dns.record.*`,
    `job.schedule`, `api.handler.register:/foo`, `config.read:my_plugin`)
  - manifest schema (`lahijan.manifest.yaml`) — declares needed permissions
  - grant table (`plugin_permissions`: plugin_id, permission, granted_by,
    granted_at) — admin-approved at install time
  - permission enforcer (intercepts every host call, checks grant, rejects
    otherwise)
- `internal/app/lahijan/wasm/installer/`:
  - upload `.wasm` + manifest
  - parse manifest, list requested permissions
  - prompt admin to approve/deny/revoke each permission
  - persist plugin + grants
- DB tables:
  - `plugins` (id, tenant_id nullable, name, version, description, wasm_hash,
    manifest_json, status (pending/active/disabled), created_at, updated_at)
  - `plugin_permissions` (plugin_id, permission, granted_by_user_id, granted_at)
- Admin API:
  - `POST /api/v1/admin/plugins/upload` (multipart: .wasm + manifest.yaml)
  - `GET /api/v1/admin/plugins` (list)
  - `GET /api/v1/admin/plugins/{id}` (detail, including manifest + grants)
  - `POST /api/v1/admin/plugins/{id}/permissions/{perm}/{grant|revoke}`
  - `POST /api/v1/admin/plugins/{id}/{enable|disable}`
  - `DELETE /api/v1/admin/plugins/{id}`

**Out of scope:**
- Actual host functions (HTTP out, KV, events) — WS-10b.
- Sample plugins + marketplace — WS-10c.
- Plugin-defined HTTP handlers (the `api.handler.register` permission lands
  here but the actual route registration lands in WS-10b).

## Required reading for the AI session

- `/AGENTS.md`
- `docs/adr/0012-wasm-full-mvp.md`
- WS-08 doc (RBAC: only platform admin can install plugins)
- `docs/architecture/conventions.md#security`
- `.opencode/skills/backend-foundations/SKILL.md`
- `.opencode/skills/database-conventions/SKILL.md`
- [wazero docs](https://wazero.io/)

## Deliverables

- wazero host runtime with memory + time limits
- Manifest parser + schema
- Permission registry (all known permissions listed)
- Plugin upload + install + grant UI flow (admin API only; full UI in WS-21)
- Permission enforcer that rejects every host call (no host funcs exist yet)
- DB schema for plugins + grants
- Audit events for: upload, install, grant, revoke, enable, disable, delete

## Definition of Done

- [x] uploading a `.wasm` parses the manifest and lists requested permissions
- [x] admin can grant/deny each permission individually
- [x] a plugin with no granted permissions loads but cannot call any host func
- [x] memory + time limits are enforced (a plugin that loops is killed)
- [x] every install/grant/revoke/enable/disable/delete emits an audit event
- [x] only `platform.admin` can hit the admin plugin API
- [x] `make lint test` green

## Open questions

- WASM target: `wasm32-unknown-unknown` vs. `wasm32-wasi`? **Resolved
  (this WS, ADR-0023):** plain `wasm32-unknown-unknown` for MVP. Plugins
  do NOT get WASI imports; every ambient capability must be a Lahijan host
  function (WS-10b), and every host function is gated through the
  permission enforcer. The manifest permission list IS the full import
  list, so the manifest is the only source of truth. WASI Preview 2 is a
  future candidate once wazero's component-model support stabilises;
  migrating will be purely additive (layer P2 on top of the existing
  permission-gated host-function surface).
- Do we sign `.wasm` modules (cosign/sigstore)? **Resolved (this WS):
  deferred.** The plugins table carries an optional `signature` BYTEA
  column so the install flow can accept and store a signed-manifest blob
  without a schema change. Verification (against a pinned public key) is
  intentionally out of scope for WS-10a; it will land alongside the
  WS-10c marketplace (where signed modules become a hard requirement).

## Notes

- This WS's permission enforcer is the lynchpin of the whole plugin security
  story. Even though no host functions exist yet, the enforcer must be in
  place so WS-10b is purely additive.
- ADR-0012 lists all planned permissions; align with that.
