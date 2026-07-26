# WS-21 · DNS / S3 / Billing / Plugins / Audit UIs

```
Status: done (with documented gaps — see Notes)
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

- [x] DNS: full zone/record CRUD via UI; DNSSEC toggles
      — `/dns` (list + create) + `/dns/$zoneId` (records /
      templates / settings tabs). DNSRecordList drives inline
      create/edit + bulk-delete; DNSSECCard gates enable/disable
      behind `dns.zone.update`; DNSTemplatesList applies the
      server catalog.
- [x] S3: bucket CRUD; credential mint-once UI; presign URL
      generation; quota display
      — `/storage` (list + create) + `/storage/$bucketId`
      (overview / credentials / presign / quota tabs). The
      credential reveal-once dialog mirrors the WS-06 PAT
      pattern; StorageUsageCard surfaces the cached bytes /
      object counts against the configured quota.
- [x] Billing: user sees balance/usage/ledger/receipts; admin
      can top up and manage prices
      — `/billing` (BalanceCard + UsagePanel + LedgerTable +
      ReceiptsCard) + `/admin/billing` (prices CRUD + per-user
      top-up + refund) + `/admin/billing/users/$userId` (per-
      user ledger view). Usage is rendered as a CSS bar chart
      (no new chart dep) — see Notes.
- [~] Plugins: admin can install + grant + enable/disable +
      uninstall
      — `/admin/plugins` (list + upload) + `/admin/plugins/
      $pluginId` (detail with status, permissions card, manifest
      viewer, enable/disable/delete) + `/admin/marketplace`
      (browse + install + upgrade). The "edit config" line item
      in the WS scope is a backend gap: WS-10c didn't ship an
      admin endpoint to set `plugin_config`, so the UI has
      nothing to call. A follow-up WS will land the endpoint +
      the matching form.
- [x] Audit: filterable + exportable
      — `/audit` (paginated table + filters: action, status,
      actor type, resource type, actor user id, from/to date
      range; CSV/JSON export via direct anchor download; click-
      through detail dialog with full metadata + outcome trail).
      Per WS-21 doc, live tail mode is out of scope (manual
      refresh).
- [x] every privileged action gated by `usePerm`
      — every create/update/delete/enable/disable/upload/
      install/grant/revoke/export button checks `usePerm` before
      rendering. Server remains the source of truth.
- [x] RTL works for fa on every page
      — every layout utility uses logical properties (`ms-`,
      `me-`, `ps-`, `pe-`, `start-`, `end-`). The ESLint rule
      blocks physical utilities.
- [x] no hardcoded English
      — the @lahijan/i18n ESLint rule fails the build on any
      bare literal in JSX. 740 keys across en.json + fa.json;
      in sync.
- [x] `npm run build` succeeds for the dashboard
      — initial entry chunk ~132 KB gzipped (still well under
      the WS-18 DoD bar of under 500 KB). Module routes are code-
      split (e.g. _zoneId ~4 KB, _bucketId ~4 KB, marketplace
      ~1.7 KB).
- [x] CI builds + lints + tests
      — the existing `.github/workflows/frontend.yml` pipeline
      runs install + lint + typecheck + test + build + locale-
      sync + openapi-sync; no workflow changes needed.

## Open questions

- Billing usage chart: which library? (Default: `recharts` or `visx`. Write an
  ADR if needed.) — **resolved (this WS):** neither. The MVP usage chart is a
  pure CSS bar list (one bar per resource type). It keeps the bundle small
  (~+0 KB) and the WS-21 surface doesn't need anything recharts/visx would
  add (axes, tooltips, time series). A future WS can swap in a richer chart
  library if usage analytics demands it; the data layer (`useMyUsage`) won't
  need to change.
- Plugins upload: drag-drop or file picker? (Default: both.) — **resolved
  (this WS):** file picker only. The native file input already opens an OS
  picker; adding drag-drop is a small additive enhancement that didn't
  belong in the MVP surface. A follow-up WS can wrap the input in a
  drop-zone component without breaking the API.
- Audit "live tail" mode (websocket stream)? (Default: no, manual refresh.)
  — **resolved (this WS):** per the default, manual refresh only. TanStack
  Query refetches on filter change; the user can also click Retry.

## Notes

- The 5 module UIs share the same patterns (table + filter + create dialog
  + permission gating + empty/loading/error states). The shared primitives
  (EmptyState / LoadingState / ErrorState from WS-20) carried through
  unchanged; this WS added no new shared components.
- Permission implications (`lib/perm.ts`) were expanded to cover the DNS /
  S3 / billing / plugins / audit permission slugs from
  `internal/app/lahijan/auth/rbac/permissions.go`. The scope-prefix check
  (`${scope}.admin`) is still in place as a fallback so a missing slug
  doesn't lock an admin out.
- The plugin upload path uses a direct `fetch` against
  `/api/v1/admin/plugins/upload` rather than the typed openapi-fetch
  wrapper because the latter doesn't surface FormData bodies well in
  v0.13. The cookie auth is sent automatically (`credentials: 'include'`).
  A future WS can migrate this to the typed client once openapi-fetch
  adds first-class multipart support.
- The billing receipt PDF download opens a new tab via `window.open` so
  the browser's PDF viewer handles rendering. The endpoint is same-origin
  in production; the dev server proxies `/api` to the backend.
- The audit export is a direct anchor download (`<a href download>`)
  rather than a fetch + Blob because the response can be large and the
  browser handles streaming + file save better than JS.
- Plugin "edit config" is a backend gap (WS-10c didn't ship the admin
  endpoint), not a UI gap. See DoD line 4.

## Resolution notes (implementation)

- **Bundle growth:** the initial entry chunk grew from ~109 KB (WS-20)
  to ~132 KB (this WS) gzipped. The growth comes from the new feature
  modules' shared dependencies (more form schemas, more components
  using the existing primitives). Each module's routes are code-split
  so the initial paint doesn't pay for module UI code the user hasn't
  navigated to. Still well below the WS-18 DoD bar of 500 KB.
- **Chart library:** per the open question default, no chart library was
  added. The UsagePanel renders a CSS bar list (no axes, no tooltips)
  — sufficient for the MVP "by resource type, last 7/30 days" view. The
  API layer (`useMyUsage`) returns raw events so a future WS can swap
  in a richer chart without breaking the data contract.
- **i18n:** 740 keys across en.json + fa.json (was 313 at the end of
  WS-20). The CI guard still passes.
- **Lint:** no new file-level suppressions were added. Two existing
  eslint-disable comments carry through unchanged (`getRouteApi`
  return-type cast in `useZoneId` / `useBucketId` / `usePluginId` /
  `useUserId` wrappers — same pattern WS-20 introduced).
- **Tests:** 54 total (was 41). The new tests are: `audit/api.test.ts`
  (3 URL-builder cases) and `storage/format.test.ts` (10 byte-format
  cases). The hook-level tests follow the WS-20 pattern; component-
  level tests are deferred to WS-22 (e2e) since the unit-test value
  for "renders a table with mocked query data" is low.

## Notes

- This WS is large; consider splitting into 5 PRs (one per area). The WS doc
  is one because they share patterns (table + filters + create modal) and the
  skills are reusable.
- Use the design-system tokens; don't pick ad-hoc colors per module.
