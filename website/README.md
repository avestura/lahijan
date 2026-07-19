# `/website`

The Lahijan marketing website — a Vite + React + TypeScript SPA, statically
prerendered at build time via `vite-react-ssg`.

See [`docs/workstreams/WS-19-marketing-website.md`](../docs/workstreams/WS-19-marketing-website.md)
and [ADR-0028](../docs/adr/0028-website-prerendering-stack.md) for the brief
and the prerendering-stack decision.

## Stack

- Vite 5 · React 18 · TypeScript (strict)
- react-router-dom v6 (routed by `vite-react-ssg`)
- Tailwind CSS + shadcn/ui primitives (shared design tokens with `web/`)
- react-i18next (en + fa, RTL)
- Vitest + React Testing Library

## Common commands

```sh
npm install           # install deps
npm run dev           # local dev server
npm run build         # typecheck + SSG build into dist/
npm run preview       # serve the built site
npm run lint          # ESLint
npm run test          # Vitest
npm run i18n:check    # assert en.json / fa.json key sync
```

The marketing site is served at `/` in production; the dashboard lives at a
separate path (`/web/` by default — see `VITE_DASHBOARD_BASE_URL`).
