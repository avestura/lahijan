#!/usr/bin/env bash
# scripts/precheck.sh — run the same Go checks CI runs (gofumpt, go vet,
# golangci-lint) before pushing or deploying.
#
# The checks run on an LF-normalised copy of the working tree (tracked +
# untracked, minus ignored files). On Windows, checkouts use CRLF and
# golangci-lint's gofumpt reports every such file as unformatted, which
# CI (Linux, LF) never sees; linting an LF copy matches CI exactly.
#
# Usage: scripts/precheck.sh            # gofumpt + go vet + golangci-lint
#        PRECHECK_FAST=1 scripts/precheck.sh   # skip golangci-lint
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

WANT_LINT_VERSION="2.5.0"

need() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "precheck: $1 not found. $2" >&2
		exit 1
	}
}
need gofumpt "Install: go install mvdan.cc/gofumpt@latest"
if [ -z "${PRECHECK_FAST:-}" ]; then
	need golangci-lint "Install: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v${WANT_LINT_VERSION}"
	have="$(golangci-lint --version | sed -n 's/.*version \([0-9.]*\).*/\1/p' | head -1)"
	if [ "$have" != "$WANT_LINT_VERSION" ]; then
		echo "precheck: golangci-lint $have found, CI uses $WANT_LINT_VERSION" >&2
		echo "  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v${WANT_LINT_VERSION}" >&2
		exit 1
	fi
fi

# Not under the system temp dir: the go command ignores go.mod there.
WORK="$REPO_ROOT/.precheck-tmp"
rm -rf "$WORK"
mkdir -p "$WORK"
trap 'rm -rf "$WORK"' EXIT

# Copy every non-frontend file; CRs are stripped from text sources only.
git ls-files -z -co --exclude-standard |
	while IFS= read -r -d '' f; do
		[ -f "$f" ] || continue
		case "$f" in web/* | website/* | docs-site/* | .precheck-tmp/*) continue ;; esac
		mkdir -p "$WORK/$(dirname "$f")"
		case "$f" in
		*.go | *.mod | *.sum | *.yml | *.yaml | *.sql | *.json | *.tmpl | *.md | *.txt | *.html | *.toml | *.conf | *.sh | *.template)
			tr -d '' <"$f" >"$WORK/$f" ;;
		*) cp "$f" "$WORK/$f" ;;
		esac
	done

cd "$WORK"

echo "precheck: gofumpt"
unformatted="$(gofumpt -extra -l . | grep -v '^\(web\|website\|docs-site\)/' || true)"
if [ -n "$unformatted" ]; then
	echo "precheck: files not gofumpt-formatted (run: gofumpt -extra -w <file>):" >&2
	echo "$unformatted" >&2
	exit 1
fi

echo "precheck: go vet"
go vet ./internal/... ./cmd/... ./pkg/...

if [ -z "${PRECHECK_FAST:-}" ]; then
	echo "precheck: golangci-lint v${WANT_LINT_VERSION}"
	golangci-lint run --timeout=10m --output.text.path=stdout ./...
fi

echo "precheck: OK"
