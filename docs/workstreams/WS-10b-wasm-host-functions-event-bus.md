# WS-10b · WASM Host Functions + Event Bus

```
Status: pending
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

- [ ] `http_request` works when `network.outbound` granted; fails when not
- [ ] `kv_*` per-plugin isolation tested (plugin A cannot read plugin B's keys)
- [ ] `emit(topic, payload)` reaches a listener in another plugin
- [ ] async event subscription survives plugin restart
- [ ] `schedule(name, args, run_at)` runs the WASM function at the right time
- [ ] `register_handler` mounts a new HTTP route dynamically (and removes it
      when the plugin is disabled)
- [ ] `config_get` returns admin-set values, never secrets
- [ ] every host function call is observable in OTel traces
- [ ] `make lint test` green

## Open questions

- HTTP outbound URL allowlist: glob (`*`) or regex? (Default: glob.)
- Event payload size cap? (Default: 64 KiB; larger via KV pointer.)
- Can a plugin register HTTP handlers under any path, or only under
  `/api/v1/plugins/{plugin-slug}/...`? (Default: the latter, for safety.)

## Notes

- This WS delivers the bulk of the value of the plugin system. Once it lands,
  WS-10c is "just" sample plugins + polish.
