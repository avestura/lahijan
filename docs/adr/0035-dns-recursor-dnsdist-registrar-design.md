# ADR-0035: DNS Recursor + dnsdist + Registrar Resale design (WS-28)

- **Status:** Accepted
- **Date:** 2026-07-21
- **Deciders:** maintainer

## Context

WS-28 ("DNS Recursor + dnsdist + Registrar Resale") lifts Lahijan's DNS
surface from authoritative-only (WS-12) to a full DNS stack:

1. **PowerDNS Recursor** — resolving service for users who want Lahijan to
   resolve on their behalf (caching, RPZ, forwarding).
2. **dnsdist** — protocol-aware DNS load-balancer in front of the recursor
   + the authoritative server (WS-12). Provides rate-limiting, source-ACL
   routing, and a single public 53/udp+tcp port of entry.
3. **Registrar resale** — end users register / renew / transfer domains
   through Lahijan; the platform resells a real registrar's API
   (ResellerClub, OpenSRS, ...) under the hood.

Three sub-decisions land in this ADR.

### Sub-decision A — Recursor + dnsdist topology

Options considered:

- **Option A1 — Single PowerDNS daemon in `recursor` mode.** Pros: one
  process. Cons: impossible to run authoritative + recursor in the same
  daemon (PDNS forces a mode choice at startup); would lose authoritative.
- **Option A2 — Two separate PowerDNS daemons (`auth` + `recursor`) on
  different ports, no front LB.** Pros: simple. Cons: exposes two ports
  to the public internet; no rate-limiting; no protocol-aware routing
  (auth vs. recursion chosen by which port the client hits).
- **Option A3 — `auth` + `recursor` + `dnsdist` in front.** Pros:
  dnsdist is the upstream-recommended DNS load-balancer (PowerDNS
  project); protocol-aware routing (auth-zone queries go to `auth`,
  everything else goes to `recursor`); rate-limiting; source-ACL routing;
  single public 53 port of entry. Cons: one more container.

### Sub-decision B — Registrar driver library choice

Same shape as ADR-0025 (Incus), ADR-0026 (PowerDNS), ADR-0027 (SeaweedFS):
no upstream Go SDK is canonical for any single registrar, and every
popular registrar exposes its own HTTP API shape. Pulling a third-party
client for one registrar would lock Lahijan into that registrar.

Options considered:

- **Option B1 — Third-party SDK per registrar (e.g.
  `github.com/.../opensrs-go`).** Pros: typed end-to-end for one
  registrar. Cons: locks Lahijan into that registrar; spotty release
  cadence; painful to drive against an httptest fake; does not match
  the Phase-3 pattern.
- **Option B2 — Thin internal `RegistrarProvider` interface + at least
  one in-tree implementation.** Pros: registry-of-implementations
  pattern matches how the auth IdP stack (WS-07a) handles multiple
  OAuth/OIDC providers; tests stub via httptest; deployers bring their
  own registrar account (per WS-28 "Notes"). Cons: we own the request
  / response shapes (mitigation: registrar HTTP APIs are stable + well
  documented; we keep the per-provider surface narrow).

### Sub-decision C — DNSSEC automation on domain registration

WS-28 requires "when a user registers a domain, automatically publish
DS at the parent and sign the zone." The signing side is already
covered by WS-12's `EnableDNSSEC`; the DS-at-parent side requires the
registrar driver to expose `SetDSRecords(domain, ds)`.

The platform decision is that **DNSSEC auto-enable is OFF by default**
on domain registration; the user opts in via the registration request
(or flips it later via the existing `dns.zone.dnssec.enable` endpoint).
This mirrors WS-12's "Open questions" item 1 (DNSSEC off by default
for new zones) so the platform is uniform.

## Decision

WS-28 ships:

1. **Compose topology** — two new services:
   - `powerdns-recursor` (`powerdns/pdns-recursor-49:4.9.3`) on the
     internal recursor port (5300). Forwards to the authoritative daemon
     for zones Lahijan serves; recurses for everything else. Optional,
     enabled via the `dns.recursor.enabled` compose profile (some
     operators do not want Lahijan to be a recursive resolver).
   - `dnsdist` (`powerdns/dnsdist-17:1.7.7`) on the public DNS port
     (53/udp+tcp). Routes by zone: queries for authoritative zones →
     `powerdns:53`; everything else → `powerdns-recursor:5300` (when
     present) or REFUSED.
2. **Registrar driver** — `internal/app/lahijan/providers/registrar/`:
   - `Provider` interface (`Name / Ping / Capabilities` + registrar
     methods `CheckDomain / RegisterDomain / RenewDomain /
     TransferDomain / SetDSRecords / GetDomain`).
   - One in-tree implementation: `OpenSRSProvider` (the OpenSRS / Tucows
     Reseller API is the WS-28 doc's named default). Sourced from
     `providers.registrar.openSRS.*` config.
   - A `Fake` (httptest) under `providers/registrar/fake/` for tests.
3. **Registrar service** — `internal/app/lahijan/registrar/` with the
   domain lifecycle orchestration. Charges land via the existing
   `billing.Service.PostCharge` (per ADR-0013's ledger-only model).
4. **DNSSEC automation seam** — the registrar interface includes
   `SetDSRecords`; the service composes `EnableDNSSEC` (WS-12) +
   `SetDSRecords` so a single `RegisterDomain{autoDNSSEC:true}` request
   signs the zone and submits the DS at the parent. The composition is
   best-effort: a DS submit failure does not roll back the signing.
5. **DB schema** — one tenant-scoped table `dns_domains` (migration
   0046) tracking the per-tenant domain lifecycle (registered, renewed,
   transferred, etc.). The PowerDNS authoritative `dns_zones` table
   (WS-12) continues to back zone + RRset storage; `dns_domains` is the
   ownership + lifecycle record.
6. **Public API** — `/api/v1/dns/domains/*` (search, register, renew,
   transfer, list, get, delete). Every privileged endpoint calls
   `RequirePerm` and emits audit pre + post. Internal infrastructure
   names ("PowerDNS", "OpenSRS", "ResellerClub") NEVER appear in the
   API or UI (per pillar 1).

## Consequences

- **Positive:** uniform with the Phase-3 driver shape (Incus / PowerDNS /
  SeaweedFS) — a maintainer reading any provider sees the same shape.
- **Positive:** zero new runtime dependencies for the registrar path:
  the package imports only stdlib + `go.opentelemetry.io/otel`.
- **Positive:** deployers pick their registrar; Lahijan ships the
  abstraction + one default implementation.
- **Positive:** dnsdist gives protocol-aware rate-limiting for free;
  Lahijan's HTTP rate-limiter (Fiber middleware) does not need to be
  taught DNS.
- **Negative:** we own the registrar request/response shapes. Mitigation:
  OpenSRS' HTTP API is documented + stable; types in the driver are
  intentionally minimal.
- **Negative:** the recursor + dnsdist containers add ~50 MiB of image
  to the prod stack. Mitigation: both are gated behind the
  `dns.recursor.enabled` profile so a minimal install still ships just
  `auth`.

## Compliance

- `deployments/docker-compose.dev.yml` + `docker-compose.prod.yml`
  declare the `powerdns-recursor` + `dnsdist` services, both gated
  behind the `dns.recursor.enabled` profile.
- `deployments/dnsdist/dnsdist.conf.template` ships the
  protocol-aware routing config (auth-zone → `powerdns:53`; default →
  `powerdns-recursor:5300`).
- `internal/app/lahijan/providers/registrar/` is the single home for
  registrar drivers; `Provider` interface + `OpenSRSProvider` +
  `fake.Server` live there.
- `internal/app/lahijan/registrar/` is the service module; every
  privileged method calls audit pre + post and emits into the WASM
  event bus.
- `internal/app/lahijan/database/migrations/0046_dns_domains.up.sql`
  + `0046_dns_domains.down.sql` declare the tenant-scoped table.
- `api/openapi.yaml` declares every `/api/v1/dns/domains/*` route; CI
  verifies the generated Go + TS clients match (per ADR-0015).
- No third-party Go module is added for the registrar path. `go.mod`
  does not grow any new entry for WS-28.

## References

- ADR-0007 (shared Postgres — the `pdns` logical DB the auth + recursor
  daemons share)
- ADR-0010 (full Incus surface — sister decision; WS-28 mirrors the
  shape for DNS)
- ADR-0013 (ledger billing — charges land via the existing ledger)
- ADR-0015 (REST + OpenAPI as source of truth)
- ADR-0025 (Incus client library choice — the pattern this ADR mirrors)
- ADR-0026 (PowerDNS client library choice — the pattern this ADR
  mirrors for the registrar driver)
- [PowerDNS Recursor docs](https://doc.powerdns.com/recursor/)
- [dnsdist docs](https://dnsdist.org/)
- [OpenSRS Reseller API](https://docs.opensrs.com/)
- WS-12 (PowerDNS provider), WS-15 (DNS module), WS-17 (billing for
  registrations)
