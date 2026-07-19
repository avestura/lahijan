# ADR-0028: Marketing website prerendering stack (react-router-dom + vite-react-ssg)

- **Status:** Accepted
- **Date:** 2026-07-19
- **Deciders:** maintainer
- **Supersedes:** none (clarifies ADR-0014 + `web/AGENTS.md` for the marketing site)

## Context

ADR-0014 locks both `web/` (dashboard) and `website/` (marketing) to the same
Vite + React + TypeScript SPA stack, and explicitly anticipates that we will
add `vite-react-ssg` (or `react-snap`) to `website/` when SEO matters:

> If SEO becomes critical for the marketing site later, we'll add
> `vite-react-ssg` or `react-snap` for static prerendering — a build-time
> concern, not a rewrite.

WS-19 calls for static prerendering of every marketing route plus per-page
meta tags, sitemap.xml, and a Lighthouse SEO score ≥95. That brings ADR-0014's
"if SEO becomes critical" branch into effect.

`web/AGENTS.md` lists TanStack Router (file-based) as the routing solution for
both SPAs. TanStack Router has no first-party static-prerendering story today
(experimental Vinxi/TanStack Start is a far larger lift than MVP needs).

`vite-react-ssg` is the prerendering tool ADR-0014 named, and it is built
tightly on top of `react-router-dom` v6 — it cannot drive TanStack Router.

So the marketing site faces a choice between three paths:

- **A. TanStack Router + a router-agnostic crawler** (e.g. `react-snap`,
  Puppeteer post-build). Keeps one router; adds a heavy Puppeteer dependency
  to the build; crawling-based SSG is fragile and slow.
- **B. TanStack Router + a hand-rolled prerender script.** Most flexible;
  most maintenance; non-trivial code to get right (per-route Head, sitemap,
  links).
- **C. `react-router-dom` + `vite-react-ssg` on `website/` only.** Uses the
  tool the ADR named; deviates from the "TanStack for both apps" rule, but
  the deviation is scoped to the marketing site, which has flat routing and
  no auth/dashboard concerns.

## Decision

For `website/` (the marketing site) **only**, the routing + prerendering
stack is:

- **`react-router-dom` v6** for routing.
- **`vite-react-ssg`** for static prerendering of every route at build time.
- **`vite-react-ssg/Head`** for per-page `<title>`, meta description,
  OpenGraph, and Twitter card tags.

For `web/` (the dashboard), TanStack Router remains the locked routing
solution per `web/AGENTS.md`. The two SPAs remain on the same UI stack
(React 18, TypeScript strict, Tailwind, shadcn/ui, design tokens,
react-i18next, Zustand, TanStack Query).

This means `web/AGENTS.md`'s "Routing: TanStack Router" line is amended to
read "Routing: TanStack Router (dashboard) / react-router-dom v6 (marketing,
for SSG)".

## Consequences

- **Positive:** marketing pages get fast, SEO-friendly prerendered HTML with
  per-route meta tags out of the box; Lighthouse SEO/Performance scores are
  achievable without a heavy crawler dependency.
- **Positive:** the dashboard keeps its richer router (loaders, type-safe
  params, devtools) where that complexity pays for itself.
- **Positive:** the two apps still share the design system, i18n pipeline,
  and component patterns; only routing differs.
- **Negative:** two routing libraries in the monorepo. Engineers touching
  both apps must context-switch.
- **Negative:** `vite-react-ssg` is one more build-time dependency; it must
  track Vite's release cadence.

## Compliance

- `website/` uses `react-router-dom` + `vite-react-ssg`; `web/` continues to
  use TanStack Router.
- `website/` reuses the design tokens, Tailwind config, shadcn/ui primitives,
  and react-i18next wiring established in WS-18.
- All other frontend non-negotiables (i18n via `t()`, no physical layout
  utilities, ESLint plugin, locale sync, RTL) apply to `website/` unchanged.
- The build emits one prerendered `index.html` per route plus shared assets
  under `website/dist/`. The sitemap and `robots.txt` are generated at build
  time.

## References

- ADR-0014 (Vite SPA for both apps; pre-approved `vite-react-ssg`)
- ADR-0017 (Full i18n from day 1 — applies to both apps)
- `web/AGENTS.md` (frontend stack rules)
- `docs/workstreams/WS-19-marketing-website.md` (calls for prerendering)
- [vite-react-ssg](https://github.com/sanyuan0704/vite-react-ssg)
