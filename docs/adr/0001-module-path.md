# ADR-0001: Module path `github.com/avestura/lahijan`

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** maintainer

## Context

The working directory is `avestura.dev/lahijan` but `go.mod` declares
`github.com/avestura/lahijan`. The mismatch is confusing and needs to be
resolved before any external contributor or CI runner references the module.

Options considered:

- **`github.com/avestura/lahijan`** — current; matches the GitHub repository
  URL; zero DNS setup; Go modules "just work."
- **`avestura.dev/lahijan`** — vanity domain; needs an A/AAAA record and a
  `<meta name="go-import">` page; nice branding but adds operational overhead.

## Decision

Use `github.com/avestura/lahijan` as the Go module path for the foreseeable
future.

## Consequences

- **Positive:** zero DNS / HTTP setup; `go install github.com/avestura/lahijan/cmd/lahijan@latest` works out of the box.
- **Positive:** aligns with where the source actually lives.
- **Negative:** we lose the brand vanity path until/unless we set up `avestura.dev` later.
- **Neutral:** if we ever migrate to `avestura.dev/lahijan`, we'll need a major version bump and `go mod edit -module`.

## Compliance

`go.mod` line 1 reads `module github.com/avestura/lahijan`. All imports across
the repo use this prefix.

## References

- [Go modules vanity URLs](https://go.dev/ref/mod#vcs)
- Internal repo `go.mod`
