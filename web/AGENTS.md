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

## Layout routing (critical — easy to break)

TanStack Router's file-based routing determines layout inheritance from
**file location**, not from the URL path. A file at `routes/settings/index.tsx`
is a child of `__root`, NOT of `_auth` — even though the URL `/settings`
looks like it should be an authenticated page.

**The current architecture wraps every non-`/login` route in `AppShell` via
`__root.tsx`** (conditional on `useRouterState`). This was done because most
routes were placed at the root level rather than under `_auth/`. If you add
a new route:

1. **Option A (preferred):** put it under `routes/_auth/` so it inherits the
   auth guard via `_auth.tsx`'s `beforeLoad`.
2. **Option B:** put it at `routes/<area>/` and rely on `__root.tsx`'s
   AppShell wrapper. The page will get the sidebar + header + bootstrap
   query, but will NOT have the auth-guard redirect. Add the guard manually
   if the page requires authentication.

Never remove the `AppShell` call from `__root.tsx` without first moving
every auth-required route into `_auth/`. The symptom of a missing AppShell
is: page renders without sidebar/header, `user` is null in the session
store, and every privileged API call returns 400 `tenant_scope_required`.

## Session bootstrap (critical — silent failure mode)

The session store (`useSessionStore`) starts with `user: null`. It gets
populated by `useBootstrapSession` (a TanStack Query that GETs
`/api/v1/auth/me`), which is called from `useAuth()`, which is called from
`AppShell`.

**If `useAuth()` is not called on a page, `user` stays null forever** —
even when the cookie is valid. The page will render with empty user data,
and every tenant-scoped API call will fail with `tenant_scope_required`
because `refresh-middleware.ts` only stamps `X-Tenant-Id` when
`currentTenantId` is non-null (which is derived from `user.memberships[0]`).

Never introduce a new entry point (a new layout, a new root component) that
bypasses `AppShell` without calling `useAuth()`.

## Forms + async data (react-hook-form)

`useForm({ defaultValues })` reads the values **once at mount**. If the
form's data comes from an async source (TanStack Query, Zustand store
populated by a bootstrap query), the form will be blank until the component
unmounts and remounts.

**Use one of these patterns when form data is async:**

```tsx
// Pattern 1: useEffect + form.reset (most explicit)
useEffect(() => {
  if (data) form.reset(mapToFormValues(data));
}, [data, form]);

// Pattern 2: useForm({ values }) — react-hook-form re-syncs on change.
// Note: only works when the values object's CONTENT changes, not just
// reference identity. Use when the source is a single query .data.
const form = useForm({ values: mapToFormValues(query.data) });
```

Never use `defaultValues` alone when the data arrives after mount.

## HTTP 501 = "feature disabled" (not an error)

When a backend module is disabled (e.g. compute without Incus, marketplace
without WASM), the API returns 501. The dashboard should show a friendly
"This feature is not enabled" state, not the generic "Something went wrong"
error.

Use `isFeatureDisabledError(err)` from `@/lib/api-errors` to detect 501s
in query error branches, and render `<FeatureDisabledState>` instead of
`<ErrorState>`. Also stop polling on 501 (the operator must flip a config
flag and restart — retrying every 5s just spams logs).
