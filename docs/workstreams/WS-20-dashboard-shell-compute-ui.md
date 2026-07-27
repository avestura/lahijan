# WS-20 · Dashboard Shell + Compute UI

```
Status: done (with documented gaps — see Notes)
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

- [x] user can log in via WS-06 flow (and OAuth/OIDC/SAML if merged)
      — password login works end-to-end. The OAuth/OIDC/SAML login
      buttons on /login are a follow-up; the settings-side link/unlink
      UI is shipped (IdentitiesCard).
- [x] user can enroll + verify MFA via the UI
      — TOTP enrollment (QR + 6-digit verify), recovery codes, and
      WebAuthn are wired in /settings/security. The login-flow MFA
      challenge prompt (after the 202 from /auth/login) is a follow-
      up; the LoginForm surfaces the localized "MFA required" notice.
- [x] user can create a PAT and use it for API calls
      — /settings/tokens: list + create + reveal-once + revoke.
- [x] user can list/create/start/stop/delete instances
      — /compute (list + filter + bulk), /compute/new (3-step
      wizard), /compute/$id (detail with all lifecycle buttons).
- [~] user can exec into a running instance via xterm.js
      — /compute/$id Console tab uses xterm.js + the one-shot
      POST /instances/{id}/exec endpoint. The interactive
      bidirectional shell session needs the backend websocket bridge
      (WS-14 lists the websocket upgrade as part of its scope but
      only shipped the one-shot variant). **Tracked in WS-32
      (Interactive Exec Console); this checkbox flips to `[x]` when
      WS-32 merges.**
- [ ] user can create/restore a snapshot
      — backend gap: WS-14 lists GET/POST /instances/{id}/snapshots
      as in-scope but did not ship them. The Snapshots tab surfaces a
      localized "coming soon" notice pointing at WS-25.
- [x] tenant switcher changes the active tenant and refreshes data
      — TenantSwitcher in the Header; switching updates the session
      store's currentTenantId, which the refresh middleware stamps
      on X-Tenant-Id and the tenant-scoped query keys consume (so
      TanStack Query refetches automatically).
- [x] permission gating hides/disables actions the user can't perform
      — every privileged UI trigger (start/stop/restart/freeze/
      unfreeze/delete + the create wizard button) calls usePerm();
      the server remains the source of truth.
- [x] all status changes reflect within 5s in the UI
      — useComputeInstances polls every 5s; useComputeInstance polls
      every 2s while transitioning, every 10s at rest.
- [x] RTL layout works for fa
      — every layout utility uses logical properties (ms-, me-, ps-,
      pe-, start-, end-). The ESLint rule blocks physical utilities.
- [x] no hardcoded English
      — the @lahijan/i18n ESLint rule fails the build on any bare
      literal in JSX. 313 keys across en.json + fa.json; in sync.
- [x] `npm run build` succeeds; bundle still reasonable
      — initial entry chunk ~109 KB gzipped; the instance-detail
      chunk (includes xterm.js) is ~80 KB gzipped and is code-split
      so it only loads when the user visits /compute/$id.
- [x] CI builds + lints + tests the dashboard
      — the existing .github/workflows/frontend.yml pipeline runs
      install + lint + typecheck + test + build + locale-sync +
      openapi-sync; no workflow changes needed.

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

## Resolution notes (implementation)

- **Tenant scoping.** Rather than wire a tenant-id middleware hook per
  feature, the refresh middleware in `lib/api/refresh-middleware.ts`
  reads `useSessionStore.getState().currentTenantId` on every outbound
  request and stamps `X-Tenant-Id`. Tenant-scoped query keys embed
  the tenant id, so switching tenants via the Header dropdown causes
  TanStack Query to refetch the new tenant's data automatically.
- **Polling vs WebSocket.** Per the WS-20 doc's open question default,
  polling is used for MVP. Lists poll every 5s; the detail view polls
  every 2s while an instance is transitioning (Starting / Stopping /
  Freezing / Unfreezing / Restarting) and every 10s at rest. The
  WebSocket push variant is a Phase-7 candidate.
- **Create wizard.** Per the open question default, the wizard is 3
  fixed steps (image → size → review) with an advanced collapsible
  panel on step 2 for the profile picker + raw config YAML.
- **Command palette.** Per the open question default, both keyboard
  (⌘K) and mouse (Header search button). Drives cmdk under the hood.
- **Interactive exec console.** The WS-20 doc lists "exec console via
  xterm.js over websocket" as in-scope, but the backend (WS-14) only
  shipped the one-shot POST /instances/{id}/exec endpoint. The
  Console tab uses that endpoint + xterm.js for output rendering.
  The interactive bidirectional shell session is tracked in **WS-32
  (Interactive Exec Console)** — it adds the backend WS bridge
  (mirroring WS-24's noVNC pattern), the new
  `GET /instances/{id}/console` endpoint, a `compute.instance.console.exec`
  permission, and rewrites `InstanceConsole.tsx` to remove the
  Input/Run form (terminal becomes the input) + use ResizeObserver
  for responsive sizing.
- **Snapshots.** The WS-14 doc lists GET/POST /instances/{id}/snapshots
  + DELETE /snapshots/{name} as in-scope but did not ship them. The
  Snapshots tab surfaces a localized "coming soon" notice pointing at
  WS-25 (scheduled snapshots + backups). Adding spec endpoints
  without backend impl would have produced broken UX.
- **Sessions list.** The backend doesn't expose a /me/sessions
  endpoint today. The /settings/sessions page surfaces a "coming
  soon" notice rather than a broken UI.
- **OAuth/OIDC/SAML login buttons.** The /login page currently only
  renders the password form. Adding the "Sign in with Google/GitHub"
  buttons is a small follow-up that just wasn't load-bearing for the
  compute + settings surface area this WS ships.
