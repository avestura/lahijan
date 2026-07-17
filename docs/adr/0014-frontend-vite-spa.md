# ADR-0014: Vite SPA for both marketing + dashboard

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** maintainer

## Context

We need a frontend for both the marketing site (`website/`) and the dashboard
app (`web/`). They share a design system but serve very different audiences.

Options considered:

- **Next.js for both** — SSR/SSG; great SEO for marketing; React-based;
  more complex build; heavier hosting requirements.
- **Vite SPA for both** — simpler build; same stack for both; weaker SEO for
  marketing (mitigated with prerendering if needed).
- **Split: Next.js marketing + Vite dashboard** — best SEO + fast app; two
  build pipelines to maintain.
- **Astro marketing + Vite dashboard** — content-rich marketing; learning curve.

## Decision

Both `website/` and `web/` are **Vite + React + TypeScript SPAs**, sharing the
same design system (Tailwind + shadcn/ui + design tokens).

If SEO becomes critical for the marketing site later, we'll add `vite-react-ssg`
or `react-snap` for static prerendering — a build-time concern, not a rewrite.

## Consequences

- **Positive:** one frontend toolchain; one set of deps; one set of skills.
- **Positive:** fast dev server; fast builds; great DX.
- **Positive:** deploy both apps as static files behind any CDN.
- **Negative:** marketing site SEO is weaker than SSR out of the box.
- **Negative:** first-contentful-paint requires JS to run (mitigated by prerendering).

## Compliance

- `web/` and `website/` both have `vite.config.ts`, `package.json` with the
  same Vite + React + Tailwind + shadcn stack.
- Both share design tokens from a common package or symlink.
- Both use the OpenAPI-generated client where they call the API.

## References

- ADR-0017 (i18n — both apps must be i18n-ready from line 1)
- ADR-0015 (REST + OpenAPI — clients are generated, used by both apps)
- WS-18 (frontend foundation), WS-19 (marketing website)
