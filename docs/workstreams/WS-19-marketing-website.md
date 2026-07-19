# WS-19 · Marketing Website

```
Status: in-progress
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

- [ ] `npm run build` succeeds for `website/`
- [ ] prerendered HTML for every route
- [ ] no hardcoded English; all strings through `t()`
- [ ] en + fa in sync
- [ ] RTL layout tested for fa
- [ ] Lighthouse: Performance ≥90, Accessibility ≥95, Best Practices ≥95,
      SEO ≥95 on the production build of the landing page
- [ ] sitemap.xml valid + robots.txt sensible
- [ ] CI's `frontend.yml` builds + lints + tests website

## Open questions

- Brand identity: do we have a logo / color palette yet? (Default: defer to
  the design skills; pick a placeholder for now, swap later.)
- Pricing display: hard-coded copy or pulled from the backend's price catalog?
  (Default: hard-coded for the marketing site; the dashboard pulls live.)
- Self-host CTA: links to GitHub repo README? Docker compose command?

## Notes

- This WS is the public face of the project. Treat copy and visual hierarchy
  as first-class deliverables, not afterthoughts.
- Use `.opencode/skills/design` for any hero / banner imagery.
