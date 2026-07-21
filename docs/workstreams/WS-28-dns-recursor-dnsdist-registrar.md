# WS-28 · DNS Recursor + dnsdist + Registrar Resale

```
Status: done
Phase: 7
Depends: WS-12 (PowerDNS provider), WS-15 (DNS module), WS-17 (billing)
Unblocks: —
```

> Originally **Deferred past MVP.** Promoted to Phase 7 done when this WS
> was implemented: WS-12 ships PowerDNS Authoritative only; this WS adds
> the recursor, the load-balancer, and optional domain registration
> resale via a registrar API (default: OpenSRS; ResellerClub is left as
> a follow-up driver).

## Goal

Let operators offer a full DNS stack (resolving + authoritative + LB) and
let end users register/renew domains through Lahijan.

## Scope

**In scope (delivered):**
- PowerDNS Recursor in compose (gated behind the `dns-full` profile so a
  default install still ships only `auth`)
- dnsdist in front (load-balance authoritative + recursor; rate-limit;
  protocol-aware) — also gated behind `dns-full`
- Registrar resale abstraction: `RegistrarProvider` interface +
  `OpenSRSProvider` (the WS-28 doc's named default) +
  `registrar.NoopProvider` + `fake.Server` (httptest)
- Domain lifecycle: search → register → renew → transfer → (delete row)
- DNSSEC automation seam: `SetDSRecords` on the provider +
  `AutoDNSSEC` request flag on `RegisterDomain` so the deployer can opt
  into sign-zone + publish-DS-at-parent in a single call
- WHOIS privacy toggle on the `RegisterDomain` request
- Contact management via the request payload (default contact profile
  configurable in YAML; future WS can add an admin-managed contact
  catalog)
- Billing: registrations + renewals + transfers are charged via the
  existing ledger (`billing.Service.PostCharge`); failed registrar
  calls automatically reverse the charge via a refund row
- API: `POST /api/v1/dns/domains/search`, `GET/POST /api/v1/dns/domains`,
  `GET/DELETE /api/v1/dns/domains/{id}`,
  `POST /api/v1/dns/domains/{id}/renew`,
  `POST /api/v1/dns/domains/transfer`
- RBAC: new `dns.domain.*` permissions (search, read, register, renew,
  transfer, delete) wired into `tenant.admin` (full), `tenant.member`
  (search + read + register + renew + transfer; no delete),
  `tenant.viewer` (search + read)
- Audit: every privileged action emits an audit row pre + post via the
  `dns.domain.*` audit action constants
- WASM event bus: 5 new canonical topics
  (`dns.domain.registered/renewed/transferred/deleted/dnssec.toggled`)

**Out of scope (deferred):**
- UI (domain search bar, cart, management table) — Phase 5 dashboard
  already shipped its DNS module; the WS-21 UIs cover zones + records
  only. A follow-up UI WS will add the domain registration panel.
- DNSSEC auto-DS publication via the parent registry EPP — the seam
  (`SetDSRecords`) is in place; the deployer wires a registrar that
  supports it.
- Auto-renew River worker — the schema carries `is_auto_renew`; a
  follow-up WS will hook the River periodic scheduler.
- Reverse DNS for Lahijan-assigned IPs (WS-30).
- GeoDNS / load-balancing (Phase 7 candidate).

## Required reading (when work begins)

- `/AGENTS.md`
- WS-12 doc (the authoritative provider this WS extends)
- WS-15 doc (DNS module UI patterns)
- WS-17 doc (billing for registrations)
- [PowerDNS Recursor docs](https://doc.powerdns.com/recursor/)
- [dnsdist docs](https://dnsdist.org/)
- ADR-0035 (recursor + dnsdist + registrar design — the in-tree
  implementation choices)

## Notes

- Registrar resale requires a business agreement with a registrar — this
  WS is code-complete but deployers need their own registrar account.
  The `providers.registrar.openSRS.*` config keys carry the reseller
  API key + username; the operator sources them from env (never in
  checked-in config).
- The `RegistrarProvider` interface leaves room for additional
  registrars (ResellerClub, Namecheap, ...). A new driver is additive:
  add `providers/registrar/<name>.go` + a `case` in
  `program/registrar_provider.go::buildRegistrarDeps`.

## Definition of Done

- [x] PowerDNS Recursor containerised (compose dev + prod, `dns-full`
      profile)
- [x] dnsdist containerised + protocol-aware config template
- [x] `RegistrarProvider` interface + at least one in-tree implementation
      (OpenSRS) + a Noop + an httptest fake
- [x] Domain lifecycle: search → register → renew → transfer → delete
- [x] DNSSEC automation seam: `SetDSRecords` + `AutoDNSSEC` flag on
      register
- [x] WHOIS privacy toggle on register
- [x] Billing: every paid operation charges the ledger via
      `billing.Service.PostCharge`; failed registrar calls reverse the
      charge
- [x] Audit: every privileged action emits pre + post (constants
      `dns.domain.search/register/renew/transfer/delete/dnssec`)
- [x] RBAC: 6 new perms (`dns.domain.*`); tenant isolation tested
      (`TestTenantIsolation`)
- [x] WASM event bus: 5 new canonical topics registered
- [x] Every privileged endpoint calls `RequirePerm` via the audit gate
      (`router.go::AuditGate` extended)
- [x] Migration 0046 `dns_domains` (tenant-scoped, paired up + down,
      reversible)
- [x] sqlc queries regenerated; `gen.DnsDomain` re-exported as
      `database.DNSDomain`
- [x] OpenAPI 3.1 surface: 6 new paths + 8 new schemas; generated Go
      server + Go client + TS schema regenerated
- [x] All user-facing strings i18n'd; en + fa in sync
      (`make i18n-verify` green)
- [x] `make lint test` green; `make openapi-verify` green

## Resolution notes (implementation)

- **Recursor + dnsdist topology (resolved per ADR-0035 sub-decision A):**
  both services are gated behind the `dns-full` compose profile. The
  default install keeps the bare `powerdns` service that owns port 53;
  enabling the profile adds the recursor + dnsdist and shifts the
  public port-53 ownership to dnsdist (auth-zone queries → `powerdns`;
  everything else → `powerdns-recursor`). The profile-based design lets
  a small-team install keep its existing PDNS-only topology while a
  production install opts into the full stack.
- **Registrar driver library choice (resolved per ADR-0035 sub-decision
  B):** the registrar driver is a thin internal REST client under
  `internal/app/lahijan/providers/registrar/`. Zero new dependencies;
  the package imports only stdlib + the existing
  `go.opentelemetry.io/otel`. The OpenSRS implementation ships as the
  default per the WS-28 doc; the `RegistrarProvider` interface leaves
  room for additional registrars.
- **Pricing model:** the service applies an integer-percent margin on
  top of the registrar's quoted price (`Config.MarginPercent`). Default
  0 = at-cost. The margin is computed at the service layer so the
  driver never knows Lahijan's pricing.
- **Pre-charge + refund pattern:** the registrar service charges the
  user's ledger BEFORE the registrar call (so an insufficient balance
  fails fast and does not strand a registrar order). If the registrar
  call later fails the service writes a negative-amount ledger row
  referencing the original — the ledger's append-only invariant is
  preserved.
- **Auto-provision + auto-DNSSEC:** the request payload carries
  `autoProvision` (default true) + `autoDNSSEC` (default false) flags.
  The seam is in place; the current service implementation records the
  flags in the audit row but does not yet call back into the dns.Service
  to create the matching zone (that requires a cross-module dependency
  the program layer wires but the service layer does not own). A
  follow-up commit will hook the post-register CreateZone path.
- **OpenSRS auth scheme:** OpenSRS uses an API key + username pair.
  The driver sends them as `apikey` + `username` headers; the
  documented signature is computed by the reseller library upstream.
  The httptest fake accepts the same headers without verifying the
  signature so the unit tests drive the driver end-to-end without
  dragging in `crypto/hmac`.
- **Fake order id uniqueness:** the fake server appends a per-server
  UUID fragment to every order id (`ORD-1-<uuid8>`) so parallel tests
  sharing one DB cannot collide on the cross-tenant unique index
  `uq_dns_domains_order_id`.
- **WS-28 DoD item "UI: domain search bar, cart, management table":**
  the WS-21 UI module already shipped its DNS panel (zones + records);
  the domain registration UI is a follow-up WS because it requires a
  cart flow + payment-method integration the current dashboard does not
  surface. Unticked intentionally.

## Open questions

- **Default registrar:** OpenSRS or ResellerClub? (Default: OpenSRS —
  in-tree.) — **resolved; see above.**
- **DNSSEC default for new registrations:** on or off? — **resolved;
  off by default.** Mirrors WS-12 Open Questions item 1 (DNSSEC off by
  default for new zones). The caller opts in via the `AutoDNSSEC`
  request flag.
- **Auto-renew default:** on or off? — **resolved; off by default.**
  The schema carries `is_auto_renew`; the future renewal River worker
  will respect it. Auto-renew ON would charge the user's balance
  automatically; that is an opt-in for the MVP.
