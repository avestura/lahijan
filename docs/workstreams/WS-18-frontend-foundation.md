# WS-18 · Frontend Foundation

```
Status: done
Phase: 5
Depends on: WS-05
Unblocks: WS-19, WS-20, WS-21
```

## Goal

Scaffold the dashboard SPA in `web/` with the full locked stack: Vite, React,
TypeScript strict, Tailwind, shadcn/ui, TanStack Router/Query, Zustand,
react-hook-form + zod, react-i18next (en + fa, RTL from line 1), design
tokens, generated API client, dark/light theme. After this WS, every
subsequent UI WS just plugs in routes.

## Scope

**In scope:**
- `web/package.json` with the full locked stack (see `web/AGENTS.md`)
- `web/vite.config.ts`, `web/tsconfig.json` (strict + noUncheckedIndexedAccess)
- `web/tailwind.config.ts`, `web/postcss.config.js`
- `web/src/`:
  - `main.tsx`, `router.tsx` (TanStack Router file-based)
  - `routes/` with a starter route + auth-guarded + admin-guarded layouts
  - `components/ui/` (shadcn/ui setup; install Button, Card, Dialog, etc.)
  - `components/layout/` (AppShell, Sidebar, Header, ThemeToggle, LocaleToggle)
  - `lib/api/` (generated client from `api/openapi.yaml` via openapi-fetch)
  - `lib/queryClient.ts`, `lib/stores/session-store.ts`
  - `lib/i18n.ts` + `locales/{en,fa}.json` seeded with auth + nav strings
  - `lib/perm.ts` (the `usePerm` hook)
  - `styles/tailwind.css` + `styles/tokens.css` (light + dark, matches the
    design-system skill)
  - `hooks/` (useAuth, useTenant, useLocale, useTheme)
- ESLint + Prettier configs (flat config)
- ESLint rule blocking hardcoded strings in JSX
- CI check that `en.json` and `fa.json` are key-for-key in sync
- CI check that `openapi-fetch` schema is in sync with `api/openapi.yaml`
- Vitest + RTL setup; example test
- Theme: light + dark; persists in localStorage
- Locale: en + fa; persists; RTL flips layout automatically
- Login + logout flow wired against WS-06 (if merged; otherwise mock for now)

**Out of scope:**
- Marketing site (WS-19).
- Dashboard pages for compute/DNS/S3/billing/plugins/audit (WS-20, WS-21).
- E2E Playwright suite (WS-22).

## Required reading for the AI session

- `/AGENTS.md`
- `web/AGENTS.md`
- `docs/adr/0014-frontend-vite-spa.md`
- `docs/adr/0017-i18n-from-day-one.md`
- `docs/adr-0015-rest-openapi.md` (generated client)
- `docs/architecture/conventions.md#frontend`
- `.opencode/skills/frontend-foundations/SKILL.md`
- `.opencode/skills/ui-styling/SKILL.md`
- `.opencode/skills/design-system/SKILL.md`

## Deliverables

- A running `npm run dev` in `web/` that loads a working AppShell with sidebar
- Working light/dark theme + locale switcher (en/fa, RTL flips)
- Generated API client + working `useQuery` against `/api/v1/ping`
- Login flow that works against WS-06 endpoints
- ESLint blocking hardcoded English
- `npm run test` (Vitest) green
- `npm run build` succeeds and produces a bundle <500 KiB gzipped (sans fonts)

## Definition of Done

- [x] `npm run dev` boots without console errors
- [x] `tsc --noEmit` clean (strict mode)
- [x] `npm run lint` clean (ESLint + Prettier)
- [x] locale switch to fa flips layout to RTL
- [x] no hardcoded English strings (ESLint rule blocks them)
- [x] `en.json` and `fa.json` key-for-key identical
- [x] login flow round-trips with WS-06 backend
- [x] token + refresh handled (TanStack Query interceptor)
- [x] logout clears session
- [x] dark/light persists across reloads
- [x] design tokens match the design-system skill's three-layer architecture
- [x] CI's `frontend.yml` workflow builds + lints + tests

## Open questions

- File-based routing via TanStack Router's plugin, or programmatic? (Default:
  file-based with the TanStack Router Vite plugin.)
- shadcn/ui CLI workflow for adding components — pin a version? (Default: yes,
  pin in `package.json`.)
- Theme toggle UX: just `light`/`dark`, or `system` too? (Default: all three.)

## Notes

- This WS sets the bar for every subsequent UI WS. The token architecture,
  the i18n pipeline, the API client — they all live or die here.
- Use the `.opencode/skills/design-system/SKILL.md` for the token
  architecture. Three layers: primitive → semantic → component.
