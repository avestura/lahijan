## Summary

<!-- One paragraph: what & why. Link the issue: "Closes #123". -->

## Workstream

<!-- Which workstream is this PR part of? e.g. WS-03. See docs/workstreams/. -->

- WS: 
- Phase: 

## Type of change

- [ ] feat — new feature
- [ ] fix — bug fix
- [ ] refactor — no behaviour change
- [ ] docs — documentation only
- [ ] test — tests only
- [ ] chore — build / CI / deps
- [ ] breaking — **BREAKING CHANGE** (requires migration notes below)

## Checklist

<!--
  Every box should be ticked before requesting review, or explicitly justified
  as not applicable. CI will fail if any of these aren't met.
-->

- [ ] Commit messages follow Conventional Commits (`feat(scope): ...`)
- [ ] `make lint` passes locally
- [ ] `make test` passes locally
- [ ] New code has unit tests; integration tests where relevant
- [ ] `sqlc generate` produces no diff (if DB queries changed)
- [ ] Migrations have matching up + down and are reversible
- [ ] OpenAPI spec regenerated (if API surface changed)
- [ ] ADR written for any new architectural decision (see `docs/adr/`)
- [ ] Affected `docs/workstreams/WS-XX-*.md` updated
- [ ] No secrets, credentials, or PII in the diff

## Migration / breaking-change notes

<!-- If breaking: describe how existing deployments upgrade. -->

## Screenshots / demos

<!-- For frontend or UI-affecting changes only. -->
