# Workstream Runner

Autonomously implements workstreams one-by-one via opencode's
`ws-implementer` agent. Designed for overnight unattended runs.

## Quick start (the "I'm going to sleep" mode)

1. **Verify prereqs:**
   - Docker Desktop is running (some integration tests need it)
   - You're on `main` with a clean working tree
   - `opencode` and `golangci-lint` (v2) are on PATH
2. **Open PowerShell** (Windows PowerShell or pwsh) at the repo root.
3. **Start the runner:**
   ```powershell
   powershell -ExecutionPolicy Bypass -File scripts\Run-Workstreams.ps1
   ```
4. **Leave the window open.** Go sleep.
5. **In the morning**, see the Summary section. Failed branches are listed
   with their names; logs are in `logs/ws-runner/`.

## What it does, per workstream

1. Pre-flight: opencode on PATH, golangci-lint v2 on PATH, Docker running,
   clean tree on `main`, baseline `make lint test` is green.
2. Branch from `main` as `feat/ws-XX-<slug>`.
3. Invoke `opencode run --agent ws-implementer --auto --print-logs` with a
   prompt that follows the 9-step WS checklist.
4. Capture all output to `logs/ws-runner/WS-XX-<timestamp>.log`.
5. Hard timeout per WS (default 90 minutes).
6. Verify with `make lint test`.
7. If green AND the agent printed `WS_IMPLEMENTATION_COMPLETE: WS-XX`:
   - Commit any leftover uncommitted work.
   - `git merge --no-ff feat/ws-XX-...` to `main`.
   - Delete the branch.
   - Continue to next WS.
8. If red or no marker:
   - Leave the branch for morning review.
   - Return to `main`.
   - Skip to next WS (does NOT halt).

## Stopping gracefully

Create a STOP file:
```powershell
New-Item logs\ws-runner\STOP
```

The runner checks before each WS. The currently-running WS finishes; the next
one is skipped and the runner exits.

## Customizing the run

### Scope

```powershell
# Just Phase 0 (recommended for first overnight)
powershell -ExecutionPolicy Bypass -File scripts\Run-Workstreams.ps1 -Workstreams 'WS-03','WS-04','WS-05'

# Specific WS only
powershell -ExecutionPolicy Bypass -File scripts\Run-Workstreams.ps1 -Workstreams 'WS-06'
```

Without `-Workstreams`, the runner attempts the full MVP sequence in dependency
order (25 WSs). Realistic expectation: 2–5 WSs per overnight, depending on
model speed and WS complexity.

### Model

```powershell
powershell -ExecutionPolicy Bypass -File scripts\Run-Workstreams.ps1 -Model 'anthropic/claude-sonnet-4-6'
```

Omit `-Model` to use whatever opencode is configured to use.

### Live streaming (default ON)

By default, every line opencode writes to stdout is streamed live to your
console while the agent works. You see what it's reading, editing, and
running — no more staring at a blank `(timeout 90m)` line for an hour.

```powershell
# default: stdout streamed live
powershell -ExecutionPolicy Bypass -File scripts\Run-Workstreams.ps1

# also stream stderr (noisy; useful for debugging the agent itself)
powershell -ExecutionPolicy Bypass -File scripts\Run-Workstreams.ps1 -StreamStderr

# disable live streaming (fall back to tail-after-exit)
powershell -ExecutionPolicy Bypass -File scripts\Run-Workstreams.ps1 -LiveStream:$false
```

Implementation: opencode's stdout/stderr are redirected to log files via
`Start-Process -RedirectStandardOutput`. The runner then opens those files
with `FileShare.ReadWrite` and reads new lines as they appear in a polling
loop (100ms cadence). The timeout deadline is checked in the same loop, so
a hung agent still gets killed on schedule.

### Per-WS timeout

```powershell
# Two hours per WS
powershell -ExecutionPolicy Bypass -File scripts\Run-Workstreams.ps1 -TimeoutMinutes 120
```

### Dry run

```powershell
powershell -ExecutionPolicy Bypass -File scripts\Run-Workstreams.ps1 -DryRun
```

Runs pre-flight and prints what each WS would do, but doesn't invoke opencode.

## Morning-after checklist

1. **Check `logs/ws-runner/summary.json`** for the result of each WS.
2. **Check `git log --oneline main`** for the new merge commits (each successful
   WS lands as `merge: WS-XX <slug>`).
3. **For each failed WS**, the branch is left in place. Inspect with:
   ```powershell
   git checkout feat/ws-XX-<slug>
   git log --oneline -20
   type logs\ws-runner\WS-XX-*.log       # the agent's full output
   type logs\ws-runner\WS-XX-*.verify    # the make lint test output
   ```
4. **Fix or abandon** the failed branch. Either:
   - Continue the work manually: `git checkout feat/ws-XX-...`, fix, commit,
     `git checkout main && git merge --no-ff feat/ws-XX-...`.
   - Discard: `git branch -D feat/ws-XX-...` and re-run that WS in the next
     overnight session.
5. **Push main** when you're happy: `git push origin main`.

## Interactive alternative

For one-off WS work during the day, use the slash command inside opencode:

```
/ws WS-03
```

This invokes the `ws-implementer` agent interactively; you can answer
questions and review changes live.

## Files

| Path | Purpose |
|------|---------|
| `scripts/Run-Workstreams.ps1` | The runner itself. |
| `scripts/_ws-prompt.template` | The prompt template sent to each WS agent. |
| `.opencode/agent/ws-implementer.md` | The agent's system prompt (9-step checklist). |
| `.opencode/command/ws.md` | The `/ws` interactive slash command. |
| `logs/ws-runner/` | Per-WS logs + summary.json (gitignored). |

## Troubleshooting

- **"opencode not on PATH"** — install opencode and reopen your terminal.
- **"golangci-lint not on PATH"** — `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.0.2`.
- **"baseline 'make lint test' is red"** — fix the existing lint/test failures before running agents.
- **"working tree not clean"** — `git status` and commit or stash before running.
- **All WSs fail with same reason** — the baseline is broken or a foundational
  WS that others depend on didn't merge. Inspect the first failure carefully.
- **`Start-Job` errors** — PowerShell 5.1 (Windows PowerShell) sometimes has
  trouble with `Start-Job`. Use `pwsh` (PowerShell 7+) if available:
  `pwsh -File scripts\Run-Workstreams.ps1`.
