# AGENTS.md — Frontend (`web/` and `website/`)

> Load this **in addition to** the root `/AGENTS.md` for any UI work.

## Two SPAs

| Folder | Purpose | First WS |
|--------|---------|----------|
| `web/` | Dashboard SPA (authenticated user app: instances, DNS, S3, billing, ...) | WS-18 |
| `website/` | Marketing SPA (landing, features, pricing, about) | WS-19 |

Both are **Vite + React 18 + TypeScript (strict)**. Both share the same design
system (Tailwind + shadcn/ui + design tokens).

## Stack (locked — do not substitute)

- **Build**: Vite 5+
- **Framework**: React 18
- **Language**: TypeScript, `strict: true`, `noUncheckedIndexedAccess: true`
- **Routing**: TanStack Router (file-based)
- **Server state**: TanStack Query v5
- **Client state**: Zustand
- **Forms**: react-hook-form + zod
- **UI primitives**: shadcn/ui (Radix UI + Tailwind)
- **Styling**: Tailwind CSS, CSS vars for theme (dark/light)
- **i18n**: react-i18next + i18next, locales `en` and `fa` (fa is RTL)
- **API client**: generated from the OpenAPI spec via `openapi-typescript` +
  `openapi-fetch` (added in WS-18)
- **Testing**: Vitest + React Testing Library; Playwright for e2e (WS-22)

## Required reading before adding to the frontend

- `/AGENTS.md` — project heart
- [`docs/architecture/conventions.md#frontend`](../docs/architecture/conventions.md) — frontend rules
- `.opencode/skills/frontend-foundations/SKILL.md`
- `.opencode/skills/ui-styling/SKILL.md` — shadcn/ui patterns
- `.opencode/skills/design-system/SKILL.md` — token architecture
- ADR-0014 (Vite SPA for both apps)
- ADR-0017 (Full i18n from day one)

## Project layout (target, lands in WS-18)

```
web/
  src/
    main.tsx                  # entry; mounts <RouterProvider>
    router.tsx                # TanStack router config
    routes/                   # file-based routes
      _auth/                  # auth-guarded routes
      _admin/                 # admin-guarded routes
      instances.$id/          # instance detail page
      ...
    components/               # shared components
      ui/                     # shadcn/ui-generated primitives
      charts/
      forms/
    features/                 # one folder per domain area
      compute/
      dns/
      storage/
      billing/
      plugins/
      audit/
    hooks/                    # cross-cutting hooks (useAuth, useTenant, usePerm)
    lib/                      # api client, query client, stores, i18n
    locales/                  # en.json, fa.json
    styles/                   # tailwind.css, tokens.css
  public/
  index.html
  vite.config.ts
  tailwind.config.ts
  tsconfig.json
  package.json
```

The `website/` folder mirrors this layout but without `features/`.

## Hard rules

- **All user-facing strings go through `t()`** from `react-i18next`. No
  hardcoded English. Lint rule blocks it.
- **No direct `fetch` calls** — always go through the generated API client in
  `lib/api`. This keeps types in sync with the OpenAPI spec.
- **Server state is TanStack Query.** Don't duplicate server state in Zustand.
  Zustand is for UI-only state (theme, sidebar, modal state).
- **Every form** uses `react-hook-form` + a `zod` schema. The schema is shared
  with the OpenAPI types (generated to `lib/api/schema.ts`).
- **Every privileged UI action** must check `usePerm("scope.action")` before
  rendering the trigger. Server enforces too; this is defense in depth.
- **Both `en` and `fa` locales must stay in sync.** A PR that adds an English
  string without the Persian counterpart fails CI.
- **RTL must work.** Use logical properties (`ms-`, `me-`, `ps-`, `pe-`) not
  physical ones (`ml-`, `mr-`, `pl-`, `pr-`) for layout that flips.
