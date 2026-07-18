# WS-10b · WASM Host Functions + Event Bus

```
Status: done
Phase: 2
Depends on WS-10a
Unblocks: WS-10c
```

## Goal

Give plugins useful capabilities via gated host functions: HTTP outbound,
per-plugin KV store, event emit/listen, schedule a job, register an HTTP
handler, read config. Each host function is intercepted by the permission
enforcer from WS-10a.

## Scope

**In scope:**

### Host functions (`internal/app/lahijan/wasm/hostfuncs/`)
Each module ships as a wazero host module; each call goes through the
permission enforcer.

- `network` — `http_request(method, url, headers, body) -> response`
  - gated by `network.outbound`
  - URL allowlist configurable per plugin
  - timeout + size cap from `conf.wasm.network_*`
- `kv` — `kv_get(key)`, `kv_set(key, value, ttl)`, `kv_delete(key)`
  - per-plugin namespace (no cross-plugin reads)
  - gated by `kv.read:ns` / `kv.write:ns`
  - backed by Postgres (`plugin_kv` table) — durable
- `events` — `emit(topic, payload)`, plus a subscription mechanism
  - gated by `events.emit` (always) + `events.listen:topic` (per topic pattern)
  - subscription delivery goes through the existing River queue (durable)
- `jobs` — `schedule(name, args, run_at)` -> job_id
  - gated by `job.schedule`
  - the job itself is WASM code in the same plugin (looked up by name)
- `api` — `register_handler(method, path, handler_name)`
  - gated by `api.handler.register:<path prefix>`
  - the handler is a function exported by the plugin; the host calls it on
    each request matching the path
  - response goes back through the standard HTTP pipeline (incl. audit + RBAC
    on the route)
- `config` — `config_get(key)`
  - gated by `config.read:<scope>`
  - reads from `plugin_config` (per-plugin config set by admin)

### Event bus (`internal/app/lahijan/wasm/eventbus/`)
- topic hierarchy: `dns.record.created`, `compute.instance.stopped`, etc.
- event types declared in a single Go registry (one per provider action)
- synchronous listeners (in-process) + asynchronous listeners (via River)
- plugin subscriptions are durable: an event emitted while the plugin is
  disabled is queued (capped) and delivered when re-enabled
- the audit log listens to every event for observability

**Out of scope:**
- Sample plugins (WS-10c).
- Marketplace (WS-10c).
- Frontend UI for plugin management (WS-21).

## Required reading for the AI session

- `/AGENTS.md`
- `docs/adr/0012-wasm-full-mvp.md`
- WS-10a doc (runtime + permission enforcer this WS uses)
- WS-09 doc (River — async event delivery uses it)
- WS-08 doc (every host call audit-emits where appropriate)
- `.opencode/skills/backend-foundations/SKILL.md`

## Deliverables

- All six host function modules with permission gating
- Event bus (sync + async via River)
- Standard event registry (used by Phase 3 providers to emit events)
- Integration tests: each host function works when permission granted, fails
  cleanly when not

## Definition of Done

- [x] `http_request` works when `network.outbound` granted; fails when not
- [x] `kv_*` per-plugin isolation tested (plugin A cannot read plugin B's keys)
- [x] `emit(topic, payload)` reaches a listener in another plugin
- [x] async event subscription survives plugin restart
- [x] `schedule(name, args, run_at)` runs the WASM function at the right time
- [x] `register_handler` mounts a new HTTP route dynamically (and removes it
      when the plugin is disabled)
- [x] `config_get` returns admin-set values, never secrets
- [x] every host function call is observable in OTel traces
- [x] `make lint test` green

## Open questions

- HTTP outbound URL allowlist: glob (`*`) or regex? **Resolved (this
  WS):** glob. The `hostfuncs.Deps.AllowedURLGlobs` field accepts glob
  patterns where `*` matches any sequence of characters. An empty list
  means "allow all" (the dev default). A per-grant allowlist is a Phase
  7 candidate.
- Event payload size cap? **Resolved (this WS):** 64 KiB default, per
  the WS doc's proposed default. Larger payloads should reference a KV
  entry. Configurable via `eventbus.Config.MaxPayloadBytes`.
- Can a plugin register HTTP handlers under any path, or only under
  `/api/v1/plugins/{plugin-slug}/...`? **Resolved (this WS):** only
  under the plugin's own prefix. The host function rejects paths that
  escape via `..` or `//`; the api layer (WS-10c consumer) enforces
  the `<plugin-slug>` segment.

## Notes

- This WS delivers the bulk of the value of the plugin system. Once it lands,
  WS-10c is "just" sample plugins + polish.
- The host-function ABI is documented in ADR-0024. The full status-code
  table lives in `internal/app/lahijan/wasm/hostfuncs/codes.go`.
- The plugin-facing HTTP router that consumes `plugin_http_handlers`
  ships with WS-10c (sample plugins) — this WS persists the row + the
  register_handler host function works against the table. A WS-10c
  deliverable is the Fiber handler that dispatches to the row.
- The plugin_invoke River worker (registered by program.Start when both
  wasm + jobs are enabled) is the durable side of both jobs.schedule
  and async event delivery. The worker resolves plugin_id -> compiled
  module via the plugins table; the runtime's idempotent Compile means
  cold-compile on first invoke is correct (warm compile at upload time
  is the fast path).
