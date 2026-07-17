# WS-20 · Dashboard Shell + Compute UI

```
Status: pending
Phase: 5
Depends on: WS-18, WS-14
Unblocks: WS-21
```

## Goal

Build the actual dashboard: the app shell (sidebar, tenant switcher, profile,
settings) plus the complete Compute UI (instances list, detail, create,
lifecycle controls, xterm.js exec console). After this WS, end users can do
their day-1 compute tasks end-to-end through the browser.

## Scope

**In scope:**
- App shell:
  - sidebar with primary areas (Compute, DNS, Storage, Billing, Audit,
    Plugins (admin only), Settings)
  - top bar with tenant switcher, user menu, theme toggle, locale toggle,
    notifications bell
  - global search (Command+K) for instances / zones / buckets
- Profile + Settings:
  - profile (name, email, change password)
  - identities (OAuth/OIDC/SAML — link/unlink)
  - MFA enrollment (TOTP QR, recovery codes, WebAuthn)
  - personal access tokens (create/list/revoke)
  - sessions (list/revoke)
- Compute UI:
  - instances list with status badges, filtering, bulk actions
  - instance detail with tabs: Overview, Console (xterm.js), Snapshots,
    Network, Storage, Config, Audit
  - create instance wizard (image picker, size picker, profile picker,
    advanced options, networking)
  - lifecycle buttons (start/stop/restart/freeze/delete) with permission gating
  - exec console via xterm.js over websocket
  - snapshot create/restore/delete
- Permission gating via `usePerm()` everywhere
- Real-time updates via TanStack Query polling + WebSocket for instance state
- Empty states, loading states, error states per the design system
- All user-facing strings i18n'd (en + fa)

**Out of scope:**
- DNS / S3 / Billing / Plugins / Audit UIs (WS-21).
- noVNC for VMs (WS-24).
- Mobile-responsive deep polish (works, but desktop is primary).

## Required reading for the AI session

- `/AGENTS.md`
- `web/AGENTS.md`
- `docs/adr-0014-frontend-vite-spa.md`
- `docs/adr-0017-i18n-from-day-one.md`
- WS-06, WS-07a/b/c docs (settings + MFA UIs)
- WS-08 doc (audit)
- WS-14 doc (compute API this UI calls)
- WS-18 doc (frontend foundation this WS builds on)
- `.opencode/skills/frontend-foundations/SKILL.md`
- `.opencode/skills/ui-styling/SKILL.md`

## Deliverables

- Working dashboard with sidebar + topbar + tenant switcher
- Profile + Settings pages wired against the backend
- Full Compute UI (list + detail + create wizard + lifecycle + xterm.js
  console + snapshots)
- Permission gating on every privileged action
- Empty/loading/error states per design system
- en + fa translations

## Definition of Done

- [ ] user can log in via WS-06 flow (and OAuth/OIDC/SAML if merged)
- [ ] user can enroll + verify MFA via the UI
- [ ] user can create a PAT and use it for API calls
- [ ] user can list/create/start/stop/delete instances
- [ ] user can exec into a running instance via xterm.js
- [ ] user can create/restore a snapshot
- [ ] tenant switcher changes the active tenant and refreshes data
- [ ] permission gating hides/disables actions the user can't perform
- [ ] all status changes reflect within 5s in the UI
- [ ] RTL layout works for fa
- [ ] no hardcoded English
- [ ] `npm run build` succeeds; bundle still reasonable
- [ ] CI builds + lints + tests the dashboard

## Open questions

- Real-time instance state: WebSocket push from server, or 5s polling?
  (Default: polling for MVP; WebSocket in Phase 7.)
- Command+K palette: keyboard-first, or mouse-friendly too? (Default: both.)
- Create-instance wizard: 3-step fixed, or free-form single page? (Default:
  3-step (image → size → review) for the simple path; advanced mode in a
  collapsible panel.)

## Notes

- The compute UI is the highest-traffic surface in the dashboard. Spend the
  most design effort here.
- xterm.js over websocket: use the `@xterm/xterm` package + the addon-fit.
