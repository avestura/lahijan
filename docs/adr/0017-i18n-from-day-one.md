# ADR-0017: Full i18n from day 1 (en + fa, RTL)

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** maintainer

## Context

Lahijan's brand is Iranian (named after the city of Lahijan). Persian (fa) is
the maintainer's first locale; English (en) is the global default. Many cloud
platforms treat i18n as a "later" concern and never recover.

Options considered:

- **English-only** — simplest; closes the door on non-English users; would
  require retrofitting every string later.
- **English + Persian (RTL) from day 1** — bilingual; tests the i18n pipeline
  (RTL, plurals, interpolation) with two real languages.
- **Full i18n framework from day 1 (locale-switcher UI, translation pipeline)** —
  complete story; sets the project up for any number of locales.

## Decision

Lahijan ships **full i18n from day 1**:

- **Backend**: `go-i18n` with message bundles in `internal/app/lahijan/i18n/locales/`
  (`en.json`, `fa.json`). Every user-facing backend string (emails, notifications,
  error messages, audit descriptions) goes through the bundle.
- **Frontend**: `react-i18next` + `i18next`, with translation files under
  `web/src/locales/{en,fa}.json`. Both apps (web, website) are i18n-ready.
- **RTL support**: the `<html dir>` attribute is set from the active locale;
  Tailwind uses logical properties (`ms-*`, `me-*`, `ps-*`, `pe-*`).
- **Locale switcher UI**: from day 1.
- **CI check**: an ESLint rule blocks hardcoded strings in JSX; a script
  asserts `en.json` and `fa.json` are key-for-key in sync.

Default locale: `en`. Secondary locale: `fa`.

## Consequences

- **Positive:** the i18n pipeline is exercised from day 1, preventing the
  "we'll add it later" trap.
- **Positive:** adding a third locale later is a pure data task (translate a
  JSON file), not a refactor.
- **Positive:** RTL forces good layout discipline (logical properties).
- **Negative:** every user-facing string requires two translations.
- **Negative:** RTL testing doubles the layout QA surface.

## Compliance

- Backend: every user-facing string is loaded via `i18n.T(ctx, "key", ...)`.
- Frontend: every user-facing string goes through `t("key")` from `react-i18next`.
- CI fails if `en.json` and `fa.json` have mismatched keys.
- CI fails if hardcoded strings appear in JSX `children`.
- Tailwind config uses logical properties; an ESLint plugin flags physical
  layout utilities in shared components.

## References

- [go-i18n](https://github.com/nicksnyder/go-i18n)
- [react-i18next](https://react.i18next.com/)
- ADR-0014 (frontend stack)
- ADR-0015 (REST + OpenAPI — error messages must be localizable)
- WS-06 (core auth — first user-facing strings land here)
- WS-18 (frontend foundation — react-i18next wiring)
