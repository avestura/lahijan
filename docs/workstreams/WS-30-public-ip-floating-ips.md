# WS-30 · Public IP Assignment & Floating IPs (DEFERRED)

```
Status: deferred
Phase: 7
Depends: WS-11 (Incus provider), WS-26 (multi-node cluster)
Unblocks: —
```

> **Deferred past MVP.** The MVP uses Incus proxy devices + bridge networking;
> this WS adds real public IP assignment from the operator's pool.

## Goal

Let operators allocate a pool of public IPs and assign/release them to user
instances, including "floating" IPs that can be re-attached.

## Scope (when work begins)

- `ip_pools` table (per-operator, configurable ranges for IPv4 + IPv6)
- `floating_ips` table (per-tenant; assignment to instance)
- IP allocation logic: pick next free, reverse-DNS auto-publish (via WS-15),
  NAT/routing configuration (likely BGP via Incus or external FRR)
- Incus `proxy` device config to route public IP → instance
- Floating IP: detach + re-attach between instances; preserves the same IP
- Billing: per-IP-hour charge + egress metering
- WASM hooks: `compute.ip.assigned`, `compute.ip.released`
- UI: pool admin; per-tenant allocation list; attach/detach on instance detail
- Security: per-IP rate limiting + abuse handling

## Required reading (when work begins)

- `/AGENTS.md`
- WS-11, WS-14, WS-15, WS-17 docs
- WS-26 doc (cluster; multi-host networking)
- [Incus network forwards](https://linuxcontainers.org/incus/docs/main/howto/network_forwards/)
- [Incus proxy device](https://linuxcontainers.org/incus/docs/main/reference/devices_proxy/)

## Notes

- This WS is operationally heavy (BGP, RIR allocation, abuse handling). Most
  self-hosters won't need it; it's primarily for SaaS-style deployments.
- Floating IP semantics: detach → instance keeps running but loses the IP;
  attach to another instance → traffic switches.
