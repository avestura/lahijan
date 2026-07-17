# ADR-0010: Expose the full Incus API surface

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** maintainer

## Context

Incus is a powerful system: instances (containers + VMs), profiles, devices,
networks, network ACLs, network forwards, storage pools, storage volumes,
projects, images, cloud-init. We must decide what to expose to end users.

Options considered:

- **Abstract "instance" model** — hide container/VM distinction; user picks
  size + image; Lahijan picks type. Simple UX; throws away Incus' power.
- **Full Incus surface** — expose profiles, devices, networks, projects,
  storage pools. Power-user friendly; steeper learning curve.
- **Both (simple + advanced modes)** — UI has a "simple" path and an "advanced"
  path; maximum flexibility; double the surface area to maintain.

## Decision

Lahijan exposes the **full Incus surface** to users. Every Incus concept
(profiles, devices, networks, projects, storage pools, images, snapshots,
cloud-init) has a Lahijan API endpoint and UI surface.

Tenants are mapped to **Incus projects** (`lahijan-tenant-<uuid>`) so per-tenant
isolation is enforced by Incus itself. Within a project, users see only their
own instances, networks, etc.

## Consequences

- **Positive:** power users get the full Incus capability without Lahijan
  re-implementing abstractions.
- **Positive:** tenant isolation is delegated to Incus projects (defense in
  depth on top of ADR-0002 row-level isolation).
- **Positive:** Incus upgrades automatically unlock new features (just add
  endpoints + UI).
- **Negative:** UI complexity — we need good defaults and progressive disclosure.
- **Negative:** some Incus operations (cluster ops, server config) stay
  admin-only; we must decide per-endpoint.

## Compliance

- `internal/app/lahijan/providers/incus/` wraps the Incus Go SDK.
- `internal/app/lahijan/compute/` exposes the surface via Lahijan API.
- Tenant → Incus project mapping lives in the `tenants.incus_project_name` column.
- Every endpoint is gated by a `RequirePerm("compute.<action>")` check.

## References

- [Incus documentation](https://linuxcontainers.org/incus/docs/main/)
- [Incus projects](https://linuxcontainers.org/incus/docs/projects/)
- ADR-0002 (tenancy — tenant_id + Incus project)
- ADR-0005 (single-host topology — Incus on local socket now, cluster later)
- WS-11 (Incus provider), WS-14 (compute module)
