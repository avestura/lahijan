#!/usr/bin/env python3
"""PreToolUse hook (Bash): run the CI Go checks before anything that ships.

Blocks `git push` when scripts/precheck.sh fails, and tells Claude why. If the
LAHIJAN_DEPLOY_HOST environment variable names a host, deploys to that host
(scp/rsync/ssh to it, docker build or compose on it) are guarded the same way.
Other commands pass through untouched.
"""
import json
import os
import re
import shutil
import subprocess
import sys

# Optional: a host you deploy to (set in your own shell, never committed).
SERVER = os.environ.get("LAHIJAN_DEPLOY_HOST", "")

try:
    cmd = json.load(sys.stdin).get("tool_input", {}).get("command", "")
except Exception:
    sys.exit(0)

# Only a real command position counts, not the words inside a commit message.
is_push = re.search(r"(^|[;&|(]\s*)git\s+push\b", cmd) is not None
is_deploy = bool(SERVER) and SERVER in cmd and re.search(r"\b(scp|rsync|docker\s+(build|compose)|install\s+-m)\b", cmd) is not None
if not (is_push or is_deploy):
    sys.exit(0)

root = os.environ.get("CLAUDE_PROJECT_DIR") or os.getcwd()
# On Windows a bare "bash" can be WSL's, which lacks the Go tools; prefer Git Bash.
bash = next((p for p in ("C:/Program Files/Git/bin/bash.exe", shutil.which("bash")) if p and os.path.exists(p)), "bash")
proc = subprocess.run([bash, "scripts/precheck.sh"], cwd=root, capture_output=True, text=True)
if proc.returncode == 0:
    sys.exit(0)

out = (proc.stdout + proc.stderr).strip()
print("scripts/precheck.sh failed; fix the lint errors before pushing or deploying:\n" + out[-3500:], file=sys.stderr)
sys.exit(2)
