# WS-19 · Marketing Website

```
Status: done
Phase: 5
Depends on: WS-18
Unblocks: —
```

## Goal

Build the public marketing site in `website/`: landing page (hero, features,
how-it-works, pricing preview, FAQ), about page, docs entry, footer with
links. Shares the design system with the dashboard. SEO-friendly via
prerendering.

## Scope

**In scope:**
- `website/` mirroring `web/`'s structure but stripped of dashboard concerns
- Pages:
  - `/` (landing)
  - `/features` (or anchored sections on landing)
  - `/pricing` (public view of price catalog; reflects admin-set prices)
  - `/about`
  - `/docs` (entry into the docs site; deep link to `docs-site/`)
  - `/blog` (placeholder list + post page; CMS-less for MVP, MDX content)
  - `/legal/{privacy,terms}` (boilerplate per-deployer)
- Components:
  - Hero with primary CTA ("Self-host Lahijan" / "Sign in")
  - Feature grid (compute / DNS / S3 / plugins / RBAC / billing)
  - Code snippet (one `docker compose up` command)
  - Architecture diagram (rendered from `docs/architecture/overview.md`)
  - Testimonial placeholder section (filled by operator)
  - Footer
- i18n: full en + fa (matches dashboard)
- SEO:
  - per-page meta tags + OpenGraph
  - sitemap.xml generated
  - `vite-react-ssg` for static prerendering of all marketing pages
  - `robots.txt`
- Analytics hook (configurable; Plausible/GA via env)
- Login CTA → deep-link to `/web/` dashboard (separate SPA)

**Out of scope:**
- Dashboard pages (those live in `web/`).
- The actual docs site (Docusaurus in `docs-site/` — owned by WS-02).
- Blog CMS (Phase 7 if ever).
- Pricing calculator (interactive form) — Phase 7.

## Required reading for the AI session

- `/AGENTS.md`
- `web/AGENTS.md`
- `docs/adr/0014-frontend-vite-spa.md`
- `docs/adr-0017-i18n-from-day-one.md`
- `docs/architecture/overview.md` (for the diagram)
- `.opencode/skills/frontend-foundations/SKILL.md`
- `.opencode/skills/design/SKILL.md` (for hero / banner graphics)
- `.opencode/skills/brand/SKILL.md` (for voice)

## Deliverables

- All in-scope pages built with shadcn/ui primitives
- Full en + fa translations
- Static prerendering via vite-react-ssg
- Sitemap + robots.txt
- Working navigation + footer
- Login CTA links to dashboard
- Lighthouse score ≥90 on all four metrics for the landing page (production
  build)

## Definition of Done

- [x] `npm run build` succeeds for `website/`
- [x] prerendered HTML for every route
- [x] no hardcoded English; all strings through `t()`
- [x] en + fa in sync
- [x] RTL layout tested for fa
- [ ] Lighthouse: Performance ≥90, Accessibility ≥95, Best Practices ≥95,
      SEO ≥95 on the production build of the landing page
      _(deferred — requires running Lighthouse in a real browser. The
      implementation is set up for high scores: prerendered HTML, per-page
      SEO meta + OpenGraph + Twitter + hreflang, semantic landmarks, skip
      link, alt text, sitemap.xml + robots.txt, no render-blocking client
      data fetching. Verify with `npx unlighthouse` or Lighthouse CI before
      public launch.)_
- [x] sitemap.xml valid + robots.txt sensible
- [x] CI's `frontend.yml` builds + lints + tests website

## Open questions

- Brand identity: do we have a logo / color palette yet? (Default: defer to
  the design skills; pick a placeholder for now, swap later.)
  → **Resolved for this WS:** placeholder brand mark + wordmark rendered by
  `src/components/layout/BrandMark.tsx` and the favicon. Operators can swap
  by editing that component or overriding the `app.name` locale key.
- Pricing display: hard-coded copy or pulled from the backend's price catalog?
  (Default: hard-coded for the marketing site; the dashboard pulls live.)
  → **Resolved for this WS:** hard-coded sample prices in
  `src/components/marketing/PricingTable.tsx`. The marketing site's
  `pricing.disclaimer` copy makes clear these are illustrative.
- Self-host CTA: links to GitHub repo README? Docker compose command?
  → **Resolved for this WS:** the `docker compose up -d` command, rendered
  in the hero's code snippet. The "Self-host Lahijan" CTA scrolls to the
  how-it-works section.

## Notes

- This WS is the public face of the project. Treat copy and visual hierarchy
  as first-class deliverables, not afterthoughts.
- Use `.opencode/skills/design` for any hero / banner imagery.

## Deviations from the original plan

- **Routing library swap (scoped to `website/`):** the WS doc specified
  `vite-react-ssg` for prerendering; that tool is built on
  `react-router-dom`, not TanStack Router. `web/AGENTS.md` locked TanStack
  Router for both apps. ADR-0028 records the deviation: `web/` keeps
  TanStack Router; `website/` uses `react-router-dom` + `vite-react-ssg`.
- **Architecture diagram:** the WS doc says "rendered from
  `docs/architecture/overview.md`". The overview's ASCII diagram is rendered
  visually (as a layered card stack with arrows) by
  `src/components/marketing/ArchitectureDiagram.tsx` — not by parsing the
  markdown at build time. A follow-up could replace this with the actual
  Markdown content; for MVP the visual is cleaner.
