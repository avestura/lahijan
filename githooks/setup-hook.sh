#!/usr/bin/env bash
# Point git at the checked-in hooks (commit-msg, pre-push) instead of copying
# them into .git/hooks, so they stay current after every pull.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
git config core.hooksPath githooks
chmod +x githooks/commit-msg githooks/pre-push scripts/precheck.sh
echo "✅ Git hooks installed (core.hooksPath=githooks)"
