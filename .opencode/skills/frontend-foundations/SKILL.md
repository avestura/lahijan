---
name: frontend-foundations
description: "Use when writing frontend code (web/ dashboard or website/ marketing) for Lahijan. Triggers on .ts/.tsx edits, Vite config, Tailwind config, shadcn/ui components, react-i18next strings, TanStack Router/Query patterns, Zustand stores, react-hook-form + zod schemas."
---

# Frontend Foundations — React + TS conventions for Lahijan

Load this whenever you touch `web/` or `website/`.

## Stack (locked)

- Vite 5+ · React 18 · TypeScript strict · TanStack Router · TanStack Query v5 ·
  Zustand · react-hook-form + zod · shadcn/ui (Radix + Tailwind) ·
  react-i18next · Vitest + RTL.

No substitutions. If you need a thing this stack doesn't have, write an ADR.

## File layout

```
src/
  main.tsx                      # entry
  router.tsx                    # TanStack router
  routes/                       # file-based routes
    _auth/                      # auth-guarded
    _admin/                     # admin-guarded
    instances.$id/              # /instances/$id
  components/
    ui/                         # shadcn/ui generated (do NOT hand-edit)
    charts/ forms/ layout/
  features/<area>/              # one folder per domain (compute, dns, ...)
    api.ts                      # area-specific query hooks
    components/
    forms/
    schemas.ts                  # zod schemas for the area
    types.ts
  hooks/                        # cross-cutting hooks
  lib/                          # api client, query client, stores, i18n
  locales/{en,fa}.json          # translations (kept in sync)
  styles/{tailwind.css, tokens.css}
```

## Hard rules

### i18n (no exceptions)

- Every user-visible string passes through `t("key")` from `react-i18next`.
- An ESLint rule blocks string literals in JSX `children` and most props.
- `en.json` and `fa.json` must stay key-for-key in sync. CI fails otherwise.
- Keys are dot-notation, area-prefixed: `compute.instances.list.emptyState`.
- Don't put interpolation in keys; use `t("key", { count: 5 })`.

### RTL (Persian is RTL)

- Use **logical properties** in Tailwind: `ms-*`, `me-*`, `ps-*`, `pe-*`,
  `start-*`, `end-*` — NOT `ml-*`, `mr-*`, `pl-*`, `pr-*`, `left-*`, `right-*`.
- The `<html dir>` attribute is set from the active locale.
- Test with `dir=rtl` before claiming a UI is done.

### API access (no raw fetch)

- All server calls go through `lib/api/client` (generated from OpenAPI by
  `openapi-fetch`).
- Server state lives in TanStack Query. Never duplicate it in Zustand.
- Mutations invalidate the relevant query keys (use the query key factory in
  `lib/api/keys.ts`).

### Permissions

- `usePerm("scope.action")` returns `{ hasPerm, isLoading }`. Use it to
  gate every privileged UI trigger.
- This is **defense in depth** — the server is the source of truth.
- Never rely on hiding UI alone to enforce permissions.

### Forms

- `react-hook-form` + `zod` schemas.
- Schemas live in `features/<area>/schemas.ts`.
- Where the schema mirrors an API request body, derive it from the OpenAPI
  generated types in `lib/api/schema.ts` (use `z.infer`).

### Components

- shadcn/ui primitives are generated into `components/ui/`. **Don't hand-edit
  generated files** — regenerate via `npx shadcn-ui add <name>`.
- Compose primitives into area-specific components in
  `features/<area>/components/`.
- One component per file. Default export is the component.

### State management

- Server state → TanStack Query. Always.
- UI-only state local to a component → `useState`.
- Cross-component UI state (theme, sidebar, modal state) → Zustand store in
  `lib/stores/<name>-store.ts`.
- Auth/user/tenant state → Zustand store in `lib/stores/session-store.ts`
  (populated by the auth bootstrap).

### Styling

- Tailwind utilities for layout. CSS variables for theme tokens.
- Tokens defined in `styles/tokens.css` (light + dark). See the `design-system`
  skill for the three-layer token architecture.
- Dark mode via `class="dark"` on `<html>` (Tailwind's `darkMode: 'class'`).
- No CSS-in-JS. No styled-components. No emotion. CSS modules only as a
  last resort for truly one-off styles.

## Required reading

- `/AGENTS.md`
- `web/AGENTS.md`
- `docs/adr/0014-frontend-stack.md`
- `docs/adr/0017-i18n-from-day-one.md`
- `docs/architecture/conventions.md#frontend`
- `.opencode/skills/ui-styling/SKILL.md` (shadcn/ui patterns)
- `.opencode/skills/design-system/SKILL.md` (token architecture)

## Testing

- Vitest + React Testing Library for unit/component tests.
- Test file next to the source: `Foo.tsx` → `Foo.test.tsx`.
- Mock TanStack Query via `query-tests`, not by hand.
- Playwright for e2e (added in WS-22).
