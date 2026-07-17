# docs-site

Docusaurus 3 app that renders the markdown in [`../docs/`](../docs/) into a
documentation website. Source content lives in `docs/`; this folder is just
the renderer.

## Run locally

From the repo root:

```bash
make docs-install   # first time only; runs npm install here
make docs           # starts the dev server on http://localhost:3000
```

Or directly:

```bash
cd docs-site
npm install
npm start
```

## Build for production

```bash
cd docs-site
npm run build         # both locales
# or
npm run build:en      # English only
npm run build:fa      # Persian (RTL) only
```

Output lands in `docs-site/build/`.

## Locales

- `en` — default (LTR)
- `fa` — Persian (RTL)

UI translation files live at `docs-site/i18n/<locale>/`. They are *not* the
same as the engineering content translations (the markdown in `docs/` is
English-only for now; Persian translations of docs would be a future WS).

## Editing content

Edit the markdown in [`../docs/`](../docs/). Docusaurus auto-reloads on
change. Don't edit generated files; don't edit content here in `docs-site/`
except for the Docusaurus config (`docusaurus.config.ts`, `sidebars.ts`) and
the React pages + CSS under `src/`.

## Adding a custom page

Add a `.tsx` file under `src/pages/`. See `src/pages/index.tsx` for the
shape. Custom pages are NOT part of the docs markdown; they're for things
like landing pages specific to the docs site.
