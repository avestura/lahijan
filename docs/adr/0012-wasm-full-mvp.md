# ADR-0012: WASM plugin system in MVP scope

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** maintainer

## Context

Lahijan aims to be extensible by third-party developers. We need a plugin
model that is safe to run untrusted code.

Options considered:

- **Post-MVP (no plugins in MVP)** — fastest to ship MVP; loses the
  extensibility story for early adopters.
- **Foundational runtime only (sandbox, no marketplace)** — middle ground;
  plugins can be loaded but there's no install flow yet.
- **Full MVP (runtime + permission UI + sample plugins)** — full extensibility
  story; significant scope.

## Decision

Lahijan MVP ships a **full WASM plugin system** with:

- **wazero runtime** (pure Go, no CGO) — sandboxed execution of WASM modules.
- **Android-style permission manifest** — each plugin declares a
  `lahijan.manifest.yaml` listing every permission it needs
  (`network.outbound`, `kv.read:scope`, `kv.write:scope`, `events.emit`,
  `events.listen:<topic>`, `job.schedule`, `api.handler.register`,
  `config.read:scope`).
- **Admin-approved grants at install time** — the admin sees the manifest, can
  revoke individual permissions (cancelling the install), or grant them. The
  grant is stored in the DB; the runtime enforces it on every host call.
- **Host functions** that the plugin can import — each gated by permission:
  HTTP outbound, KV store (per-plugin namespace), event emit/listen, job
  schedule, HTTP handler registration, config read.
- **Sample plugins** that exercise the system: Slack notifier, autoscaler stub,
  post-DNS-record hook.
- **Plugin install UI** in the admin dashboard.

This is broken into sub-workstreams:

- **WS-10a** Runtime + permission system (sandbox, manifest, grant storage)
- **WS-10b** Host functions + event bus
- **WS-10c** Sample plugins + marketplace scaffolding

## Consequences

- **Positive:** extensibility is a first-class feature, not a bolt-on.
- **Positive:** sandboxing via WASM means untrusted code is safe to run.
- **Positive:** permission model gives admins fine-grained control.
- **Negative:** significant scope; security-critical; needs careful review.
- **Negative:** host function ABI must be stable from day 1 or break plugins.

## Compliance

- `internal/app/lahijan/wasm/` contains the runtime, permission system, host
  functions, and plugin loader.
- Every plugin install emits an audit event.
- Every plugin host call is intercepted by the permission enforcer.
- The plugin manifest schema is documented in `docs/architecture/plugins.md`.

## References

- [wazero documentation](https://wazero.io/)
- ADR-0004 (auth — admin identity required to install plugins)
- ADR-0008 (River — plugins can schedule jobs)
- WS-10a, WS-10b, WS-10c
