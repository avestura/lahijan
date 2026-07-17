# WS-21 · DNS / S3 / Billing / Plugins / Audit UIs

```
Status: pending
Phase: 5
Depends on: WS-18, WS-15, WS-16, WS-17
Unblocks: WS-22 (e2e needs these)
```

## Goal

Complete the dashboard by adding every remaining module UI: DNS zones +
records, S3 buckets + credentials + presign, Billing (balance, usage, ledger,
receipts), Plugins (admin: list, install, grant, enable/disable), Audit
(filterable log). This WS may be split into multiple PRs (one per area).

## Scope

**In scope:**

### DNS UI
- zones list + create (zone name, kind, DNSSEC toggle, template)
- zone detail with records table (filterable by type, name)
- record inline-edit + bulk delete
- DNSSEC toggle on zone
- templates picker on create

### S3 UI
- buckets list + create (name, quota, default lifetime)
- bucket detail: usage chart, credential list, presign generator (modal:
  pick method, key, expiry → copy URL)
- credential creation modal (shown once with copy button + warning)
- quota enforcement UI

### Billing UI
- user: balance card, usage chart (by resource, last 7/30 days), ledger
  table (paginated), receipts list + download
- admin: price catalog CRUD, top-up user (modal), user ledger view, refunds

### Plugins UI (admin only)
- list installed plugins with status, version, permissions granted
- upload flow: pick .wasm + manifest → review requested permissions →
  grant/deny each → install
- per-plugin detail: enable/disable, edit config, view permissions, revoke
- marketplace browser: list available → install with normal flow

### Audit UI
- paginated table with filters (tenant, actor, action, resource, date range)
- detail modal showing metadata
- export button (JSON / CSV)

**Out of scope:**
- noVNC console (WS-24).
- Real-time updates via WebSocket (Phase 7).
- Notifications UI (bell dropdown) — Phase 7.

## Required reading for the AI session

- `/AGENTS.md`
- `web/AGENTS.md`
- `docs/adr-0017-i18n-from-day-one.md`
- WS-08 doc (audit)
- WS-10a/b/c docs (plugins UI)
- WS-15 doc (DNS API)
- WS-16 doc (S3 API)
- WS-17 doc (Billing API)
- WS-18 doc (frontend foundation)
- WS-20 doc (app shell conventions to follow)
- `.opencode/skills/frontend-foundations/SKILL.md`
- `.opencode/skills/ui-styling/SKILL.md`

## Deliverables

- 5 module UIs (DNS, S3, Billing, Plugins, Audit) following WS-20's
  conventions
- All wired against the respective backend APIs
- All permission-gated
- All i18n'd (en + fa)
- Empty/loading/error states everywhere

## Definition of Done

- [ ] DNS: full zone/record CRUD via UI; DNSSEC toggles
- [ ] S3: bucket CRUD; credential mint-once UI; presign URL generation;
      quota display
- [ ] Billing: user sees balance/usage/ledger/receipts; admin can top up and
      manage prices
- [ ] Plugins: admin can install + grant + enable/disable + uninstall
- [ ] Audit: filterable + exportable
- [ ] every privileged action gated by `usePerm`
- [ ] RTL works for fa on every page
- [ ] no hardcoded English
- [ ] `npm run build` succeeds for the dashboard
- [ ] CI builds + lints + tests

## Open questions

- Billing usage chart: which library? (Default: `recharts` or `visx`. Write an
  ADR if needed.)
- Plugins upload: drag-drop or file picker? (Default: both.)
- Audit "live tail" mode (websocket stream)? (Default: no, manual refresh.)

## Notes

- This WS is large; consider splitting into 5 PRs (one per area). The WS doc
  is one because they share patterns (table + filters + create modal) and the
  skills are reusable.
- Use the design-system tokens; don't pick ad-hoc colors per module.
