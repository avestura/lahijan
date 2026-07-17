# WS-28 · DNS Recursor + dnsdist + Registrar Resale (DEFERRED)

```
Status: deferred
Phase: 7
Depends: WS-12 (PowerDNS provider)
Unblocks: —
```

> **Deferred past MVP.** WS-12 ships PowerDNS Authoritative only; this WS
> adds the recursor, the load-balancer, and optional domain registration
> resale via a registrar API (e.g. ResellerClub, OpenSRS).

## Goal

Let operators offer a full DNS stack (resolving + authoritative + LB) and
let end users register/renew domains through Lahijan.

## Scope (when work begins)

- PowerDNS Recursor in compose (for users who want Lahijan to resolve too)
- dnsdist in front (load-balance authoritative + recursor; rate-limit;
  protocol-aware)
- Registrar resale abstraction: `RegistrarProvider` interface with at least
  one implementation (default: ResellerClub or OpenSRS)
- Domain lifecycle: search → register → renew → transfer → DNSSEC auto-enable
- DNSSEC automation: when a user registers a domain, automatically publish
  DS at the parent and sign the zone
- WHOIS privacy + contact management
- Billing: registrations + renewals are charged via the existing ledger
- UI: domain search bar, cart, management table

## Required reading (when work begins)

- `/AGENTS.md`
- WS-12 doc (the authoritative provider this WS extends)
- WS-15 doc (DNS module UI patterns)
- WS-17 doc (billing for registrations)
- [PowerDNS Recursor docs](https://doc.powerdns.com/recursor/)
- [dnsdist docs](https://dnsdist.org/)

## Notes

- Registrar resale requires a business agreement with a registrar — this WS
  is code-complete but deployers need their own registrar account.
- The `RegistrarProvider` interface leaves room for additional registrars.
