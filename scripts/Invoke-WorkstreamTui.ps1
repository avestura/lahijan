<#
.SYNOPSIS
    Launch opencode (TUI mode) in a new terminal window for a workstream.

.DESCRIPTION
    Fast interactive alternative to Run-Workstreams.ps1 / Resume-Workstream.ps1.

    The existing scripts invoke `opencode run --agent ws-implementer --auto
    --print-logs` with stdout/stderr redirected to files and a 100ms polling
    loop live-streaming lines back to the parent console. That approach has
    three problems observed in production:

      1. `--print-logs` is a DEBUG mode that writes every internal opencode
         event (permission eval, path resolution, llm stream tick, ...) to
         stderr. For a verbose agent that's thousands of extra I/O events
         per run.
      2. opencode's stdout is block-buffered when redirected to a file
         (not a TTY), which breaks real-time observability and may also
         affect internal streaming decisions.
      3. `$proc.Kill()` on timeout only terminates the parent opencode
         process; its spawned server subprocess is orphaned and keeps
         running for hours (the WS-19 2026-07-19 incident showed ~13h of
         post-kill orphan activity in the stderr log).

    This launcher sidesteps all three:

      - The opencode process runs in a brand-new Windows Terminal window
        with a real TTY, so streaming works as designed.
      - `--print-logs` is NOT passed, so there's no debug-firehose I/O.
      - Closing the window kills the entire process tree (Windows
        behavior for console windows); no orphan server.

    Trade-off: the parent cannot read opencode's stdout (it lives in the
    child window). For the merge decision the parent relies on the user's
    observation plus a green `make lint test` run. This is fine for
    interactive development; for true headless runs keep using
    Run-Workstreams.ps1 (which now has the taskkill /T /F fix).

    Flow:
      1. Pre-flight (opencode + golangci-lint on PATH, clean tree, etc.)
      2. Branch setup:
           default: checkout main, reset, create feat/ws-XX-<slug>
           -Resume: checkout existing branch, commit any leftover
      3. Render prompt from _ws-prompt.template (or _ws-resume-prompt)
      4. Write a small wrapper .ps1 that:
           - sets the window title
           - reads the prompt file
           - runs opencode WITHOUT --print-logs
           - waits for a keypress before exiting
      5. Start-Process the wrapper with UseShellExecute=$true (opens a
         new window; Windows Terminal intercepts on Win11)
      6. Block on $proc.WaitForExit() (user closes window to return)
      7. Run make lint test
      8. If green + user confirms the agent emitted WS_IMPLEMENTATION_COMPLETE:
         commit leftovers, merge --no-ff, delete branch

.PARAMETER WsId
    Workstream ID, e.g. WS-21. Required.

.PARAMETER Model
    Optional provider/model override, e.g. "anthropic/claude-sonnet-4-6".

.PARAMETER Resume
    Resume an existing feat/ws-XX-<slug> branch instead of creating a new
    one. Commits any uncommitted leftover first, uses the
    _ws-resume-prompt.template.

.PARAMETER DryRun
    Set up the branch and write the prompt + wrapper files, but don't
    actually launch opencode. Useful for inspecting the generated inputs.

.PARAMETER AutoMerge
    Skip the interactive 'Merge to main? (y/N)' prompt. After opencode
    exits and `make lint test` passes, automatically commit any leftover
    tracked-file modifications and merge the branch to main (--no-ff).
    Useful when invoking the launcher from a non-interactive context
    (CI, a parent opencode session, scheduled task).

.PARAMETER NoMerge
    Opposite of -AutoMerge: never merge, even if verification passes.
    Leaves the branch as-is for manual review. Useful for first-time
    WS runs where you want to inspect the diff before merging.

.EXAMPLE
    .\scripts\Invoke-WorkstreamTui.ps1 WS-21
    # Open WS-21 in a new Windows Terminal tab; verify + merge on close.

.EXAMPLE
    .\scripts\Invoke-WorkstreamTui.ps1 WS-21 -Resume
    # Resume an in-progress WS-21 on its existing branch.

.EXAMPLE
    .\scripts\Invoke-WorkstreamTui.ps1 WS-21 -DryRun
    # See what would be launched without actually launching it.

.EXAMPLE
    .\scripts\Invoke-WorkstreamTui.ps1 WS-21 -AutoMerge
    # Run + auto-merge on green verification, no prompt. Use this when
    # invoking the launcher from another opencode session or a script.
#>

[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)]
    [string]$WsId,

    [string]$Model,

    [switch]$Resume,

    [switch]$DryRun,

    [switch]$AutoMerge,

    [switch]$NoMerge
)

$ErrorActionPreference = "Stop"
$ProgressPreference    = "SilentlyContinue"

# ---------------------------------------------------------------------------
# Helpers (mirror Run-Workstreams.ps1 / Resume-Workstream.ps1)
# ---------------------------------------------------------------------------

function Write-Header  { param($Text) Write-Host ""; Write-Host ("=" * 70) -ForegroundColor DarkCyan; Write-Host $Text -ForegroundColor Cyan; Write-Host ("=" * 70) -ForegroundColor DarkCyan }
function Write-Step    { param($Text) Write-Host ">>> $Text" -ForegroundColor Yellow }
function Write-OK      { param($Text) Write-Host "[OK]   $Text" -ForegroundColor Green }
function Write-Skip    { param($Text) Write-Host "[SKIP] $Text" -ForegroundColor DarkYellow }
function Write-Fail    { param($Text) Write-Host "[FAIL] $Text" -ForegroundColor Red }
function Write-Info    { param($Text) Write-Host "       $Text" -ForegroundColor DarkGray }

function Invoke-Git {
    param([string[]]$ArgList)
    $prev = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        & git @ArgList 2>&1 | Out-Null
        return ($LASTEXITCODE -eq 0)
    } finally {
        $ErrorActionPreference = $prev
    }
}

function Invoke-GitCapture {
    param([string[]]$ArgList)
    $prev = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $out = & git @ArgList 2>&1 | Out-String
        return $out
    } finally {
        $ErrorActionPreference = $prev
    }
}

function Get-WsSlug {
    param([string]$WsId)
    $files = Get-ChildItem -Path "docs/workstreams" -Filter "$WsId-*.md" -ErrorAction SilentlyContinue
    if (-not $files -or $files.Count -eq 0) { return "impl" }
    $name = $files[0].BaseName
    $prefix = "$WsId-"
    if ($name.StartsWith($prefix)) { return $name.Substring($prefix.Length) }
    return "impl"
}

# ---------------------------------------------------------------------------
# Pre-flight
# ---------------------------------------------------------------------------

Write-Header "Invoke-WorkstreamTui - pre-flight"

if (-not (Get-Command opencode -ErrorAction SilentlyContinue)) {
    throw "opencode is not on PATH."
}
Write-OK "opencode on PATH"

$gopath = go env GOPATH
$lintPath = Join-Path $gopath "bin"
if (-not ($env:Path -split ';' -contains $lintPath)) {
    $env:Path = "$lintPath;$env:Path"
}
if (-not (Get-Command golangci-lint -ErrorAction SilentlyContinue)) {
    throw "golangci-lint not on PATH. Run: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.0.2"
}
Write-OK "golangci-lint on PATH"

$slug       = Get-WsSlug $WsId
$branchName = "feat/$($WsId.ToLower())-$slug"
$wsDocPath  = "docs/workstreams/$WsId-$slug.md"

if (-not (Test-Path $wsDocPath)) {
    throw "WS doc not found: $wsDocPath"
}

# ---------------------------------------------------------------------------
# Branch setup
# ---------------------------------------------------------------------------

$aheadCount = 0

if ($Resume) {
    Write-Header "$WsId ($slug) - resume"

    $branchList = Invoke-GitCapture 'branch','--list',$branchName
    if (-not $branchList.Trim()) {
        throw "branch $branchName does not exist. Run without -Resume to start $WsId from scratch."
    }
    Write-OK "branch $branchName exists"

    if (-not (Invoke-Git 'checkout',$branchName)) {
        throw "couldn't check out $branchName"
    }
    Write-OK "on branch $branchName"

    $aheadRaw = Invoke-GitCapture 'rev-list','--count',"main..$branchName"
    $aheadCount = [int]$aheadRaw.Trim()
    $behindRaw = Invoke-GitCapture 'rev-list','--count',"$branchName..main"
    $behindCount = [int]$behindRaw.Trim()
    if ($aheadCount -eq 0) {
        Write-Skip "branch has no commits ahead of main; resuming anyway"
    } else {
        Write-OK "branch is $aheadCount commit(s) ahead of main"
        Invoke-GitCapture 'log','--oneline','-5',"main..$branchName" |
            ForEach-Object { Write-Info "  $_" }
    }
    # Stale-branch guard: if the branch is significantly behind main, the
    # WS will be missing recent context (AGENTS.md updates, new ADRs, helper
    # scripts, etc.). The agent will be operating on outdated foundations.
    # 30 commits behind is ~2-3 merged WS worth of drift; above that, refuse.
    if ($behindCount -gt 30) {
        Write-Fail "branch is $behindCount commit(s) BEHIND main (merge-base is very old)"
        Write-Info "this branch predates significant recent work and is unsafe to resume"
        Write-Info "to start fresh on a new branch from current main:"
        Write-Info "  git branch -D $branchName"
        Write-Info "  .\scripts\Invoke-WorkstreamTui.ps1 -WsId $WsId"
        throw "stale branch; refusing to resume (see above)"
    } elseif ($behindCount -gt 5) {
        Write-Skip "warning: branch is $behindCount commit(s) behind main"
        Write-Info "consider rebasing onto main if the agent hits missing-context errors"
    }

    $uncommitted = Invoke-GitCapture 'status','--porcelain'
    if ($uncommitted.Trim()) {
        # CRITICAL: use `git add -u` (only tracked modifications), NOT `git add -A`.
        # `git add -A` would also stage untracked junk files like web/src/routeTree.gen.ts
        # (a TanStack Router generated file that's gitignored but in some checkouts
        # slips through) or random debugging artifacts. The leftover-commit is only
        # meant as a safety net for tracked files the agent forgot to commit.
        Write-Step "committing tracked-file modifications from prior session"
        Invoke-Git 'add','-u'
        Invoke-GitCapture 'commit','-m',("wip: $WsId leftover from prior session (Invoke-WorkstreamTui)") | Out-Null
        Write-OK "leftover committed"
        # Warn about untracked files left behind.
        $stillUntracked = (Invoke-GitCapture 'status','--porcelain') -split "`n" | Where-Object { $_ -match '^\?\?' }
        if ($stillUntracked) {
            Write-Skip "untracked files left as-is (review manually if needed):"
            $stillUntracked | ForEach-Object { Write-Info "  $_" }
        }
    }

    $templatePath = Join-Path $PSScriptRoot "_ws-resume-prompt.template"
    $modeLabel = "resume"
} else {
    Write-Header "$WsId ($slug) - fresh"

    $dirty = Invoke-GitCapture 'status','--porcelain'
    if ($dirty.Trim()) {
        Write-Host $dirty
        throw "working tree not clean. Commit or stash before running."
    }
    Write-OK "working tree clean"

    $branch = (Invoke-GitCapture 'rev-parse','--abbrev-ref','HEAD').Trim()
    if ($branch -ne "main") {
        throw "on branch '$branch'; switch to main first."
    }
    Write-OK "on main"

    if (-not (Invoke-Git 'checkout','main')) { throw "couldn't checkout main" }
    if (-not (Invoke-Git 'reset','--hard','HEAD')) { throw "couldn't reset main" }
    $null = Invoke-Git 'branch','-D',$branchName
    if (-not (Invoke-Git 'checkout','-b',$branchName)) {
        throw "couldn't create branch $branchName"
    }
    Write-OK "on branch $branchName"

    $templatePath = Join-Path $PSScriptRoot "_ws-prompt.template"
    $modeLabel = "fresh"
}

if (-not (Test-Path $templatePath)) {
    throw "missing prompt template: $templatePath"
}

# ---------------------------------------------------------------------------
# Render prompt
# ---------------------------------------------------------------------------

$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
$logDir    = "logs/ws-runner"
New-Item -ItemType Directory -Path $logDir -Force | Out-Null

$promptFile  = Join-Path $logDir "$WsId-tui-$timestamp.prompt"
$wrapperFile = Join-Path $logDir "$WsId-tui-$timestamp.wrapper.ps1"
$verifyFile  = Join-Path $logDir "$WsId-tui-$timestamp.verify"

$promptTemplate = Get-Content -Path $templatePath -Raw -Encoding utf8
$prompt = $promptTemplate `
    -replace '\{\{WS_ID\}\}',       $WsId `
    -replace '\{\{SLUG\}\}',        $slug `
    -replace '\{\{REPO_ROOT\}\}',   (Get-Location).Path `
    -replace '\{\{BRANCH\}\}',      $branchName `
    -replace '\{\{WS_DOC_PATH\}\}', $wsDocPath `
    -replace '\{\{AHEAD_COUNT\}\}', $aheadCount

$prompt | Out-File -FilePath $promptFile -Encoding utf8

# ---------------------------------------------------------------------------
# Build the wrapper script that runs in the new window
# ---------------------------------------------------------------------------

# Build the opencode args. NOTE: --print-logs is intentionally absent.
# That flag is a debug mode that adds substantial per-event I/O overhead.
# For interactive runs the default stdout stream is both faster and more
# readable.
$ocArgs = @('run','--agent','ws-implementer','--auto')
if ($Model) { $ocArgs += @('-m', $Model) }

$ocPath    = (Get-Command opencode).Source
$repoRoot  = (Get-Location).Path
$title     = "$WsId - $slug ($modeLabel)"
$ocArgsQ   = ($ocArgs | ForEach-Object { '"' + ($_ -replace '"','\"') + '"' }) -join ' '

# The wrapper does:
#   1. Set window title + print banner.
#   2. Read the prompt file (multi-line string).
#   3. Invoke opencode with TTY-attached stdout (full streaming speed).
#   4. Wait for a keypress so the user can review the tail before exit.
#   5. Exit with opencode's exit code so the parent can read it.
#
# We MUST pass $prompt as a single native arg. PowerShell 7+ handles
# multi-line strings correctly when invoking native exes; on Windows
# PowerShell 5.x there are edge cases, but we're targeting `pwsh` per
# the project's shell convention.
# Build the wrapper tail. In interactive mode (no -AutoMerge), the wrapper
# waits for a keypress after opencode exits so the user can review the tail
# of the output before the window closes. In autonomous mode (-AutoMerge),
# the wrapper closes immediately so the parent script can continue to the
# next WS without human intervention.
if ($AutoMerge) {
    $tailBlock = @"
Write-Host '  Closing window automatically (-AutoMerge); parent will verify + merge...' -ForegroundColor DarkGray
Write-Host '===================================================================' -ForegroundColor Cyan
Start-Sleep -Seconds 2
exit `$ocExit
"@
    $bannerCloseLine = '  Window will close automatically after opencode exits (-AutoMerge).'
} else {
    $tailBlock = @"
Write-Host '  Press any key to close this window...' -ForegroundColor Yellow
Write-Host '===================================================================' -ForegroundColor Cyan
`$null = `$host.UI.RawUI.ReadKey('NoEcho,IncludeKeyDown')
exit `$ocExit
"@
    $bannerCloseLine = '  Then press any key to close. The parent script will run'
}

$wrapperBody = @"
`$ProgressPreference = 'SilentlyContinue'
`$ErrorActionPreference = 'Continue'
try { `$host.UI.RawUI.WindowTitle = '$title' } catch {}
Set-Location -LiteralPath '$repoRoot'

Write-Host ''
Write-Host '===================================================================' -ForegroundColor Cyan
Write-Host '  $WsId - $slug  (mode: $modeLabel)' -ForegroundColor Cyan
Write-Host '  opencode run --agent ws-implementer --auto' -ForegroundColor DarkCyan
if ('$Model' -ne '') { Write-Host "  model: $Model" -ForegroundColor DarkCyan }
Write-Host "  prompt:  $promptFile" -ForegroundColor DarkGray
Write-Host "  branch:  $branchName" -ForegroundColor DarkGray
Write-Host "  ws doc:   $wsDocPath" -ForegroundColor DarkGray
Write-Host '-------------------------------------------------------------------' -ForegroundColor DarkGray
Write-Host '  When opencode finishes, look for this line in the output:' -ForegroundColor DarkGray
Write-Host '    WS_IMPLEMENTATION_COMPLETE: $WsId' -ForegroundColor Green
Write-Host "  $bannerCloseLine" -ForegroundColor DarkGray
Write-Host '===================================================================' -ForegroundColor Cyan
Write-Host ''

`$prompt = Get-Content -LiteralPath '$promptFile' -Raw -Encoding UTF8
& '$ocPath' $ocArgsQ `$prompt
`$ocExit = `$LASTEXITCODE

Write-Host ''
Write-Host '===================================================================' -ForegroundColor Cyan
if (`$ocExit -eq 0) {
    Write-Host "  opencode exited cleanly (code `$ocExit)" -ForegroundColor Green
} else {
    Write-Host "  opencode exited with code `$ocExit" -ForegroundColor Red
}
$tailBlock
"@

$wrapperBody | Out-File -FilePath $wrapperFile -Encoding utf8

if ($DryRun) {
    Write-Skip "DRY RUN - files written, opencode NOT launched"
    Write-Info "mode:       $modeLabel"
    Write-Info "branch:     $branchName"
    Write-Info "model:      $(if ($Model) { $Model } else { '(default)' })"
    Write-Info "prompt:     $promptFile"
    Write-Info "wrapper:    $wrapperFile"
    Write-Info "ws doc:     $wsDocPath"
    Write-Host ""
    Write-Host "Prompt preview (first 20 lines):" -ForegroundColor DarkGray
    Get-Content $promptFile -Head 20 | ForEach-Object { Write-Host "    $_" -ForegroundColor DarkGray }
    Write-Host ""
    Write-Host "Wrapper preview (first 25 lines):" -ForegroundColor DarkGray
    Get-Content $wrapperFile -Head 25 | ForEach-Object { Write-Host "    $_" -ForegroundColor DarkGray }
    return
}

# ---------------------------------------------------------------------------
# Launch the wrapper in a new window
# ---------------------------------------------------------------------------

Write-Step "launching opencode in new window"
Write-Info "wrapper:  $wrapperFile"
Write-Info "prompt:   $promptFile"
Write-Info "branch:   $branchName"
Write-Info "model:    $(if ($Model) { $Model } else { '(default)' })"
Write-Host ""
Write-Host "  A new Windows Terminal window will open with opencode running." -ForegroundColor Cyan
Write-Host "  Watch for: WS_IMPLEMENTATION_COMPLETE: $WsId" -ForegroundColor Green
Write-Host "  To abort:  close the opencode window." -ForegroundColor DarkGray
Write-Host ""

# Start-Process with UseShellExecute=$true opens a new console window.
# Windows Terminal (if set as the default terminal app, Win11 default)
# intercepts the spawn and hosts the process in a WT tab/window, giving
# opencode a real TTY for streaming.
#
# We can't use `wt.exe new-tab ...` because:
#   - If a WT window is already running, wt.exe returns immediately after
#     opening the new tab, giving us no PID to wait on.
#   - We need the launched process's PID to block on WaitForExit().
$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName               = 'powershell.exe'
$psi.Arguments              = "-NoProfile -ExecutionPolicy Bypass -File `"$wrapperFile`""
$psi.UseShellExecute        = $true
$psi.WindowStyle            = 'Normal'
$psi.WorkingDirectory       = $repoRoot

$proc = [System.Diagnostics.Process]::Start($psi)
if (-not $proc) {
    throw "couldn't start opencode window"
}

Write-Step "waiting for opencode window (PID $($proc.Id)) to close"
Write-Host ""

$proc.WaitForExit()
$ocExit = $proc.ExitCode

Write-Host ""
if ($ocExit -eq 0) {
    Write-OK "opencode exited cleanly (code $ocExit)"
} else {
    Write-Fail "opencode exited with code $ocExit (continuing to verify anyway)"
}

# ---------------------------------------------------------------------------
# Verify with make lint test
# ---------------------------------------------------------------------------

Write-Step "verifying: make lint test"
$prev = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
& make lint test 2>&1 | Tee-Object -FilePath $verifyFile | Out-Host
$verifyExit = $LASTEXITCODE
$ErrorActionPreference = $prev

Write-Info "verify exit: $verifyExit"

if ($verifyExit -ne 0) {
    Write-Fail "$WsId verification did not pass"
    Write-Info "branch $branchName left as-is"
    Write-Info "fix issues, then run:"
    Write-Info "  .\scripts\Invoke-WorkstreamTui.ps1 -WsId $WsId -Resume"
    return
}

# ---------------------------------------------------------------------------
# Confirm merge with the user (unless -AutoMerge or -NoMerge)
# ---------------------------------------------------------------------------

if ($NoMerge) {
    Write-Skip "-NoMerge set; branch $branchName left as-is even though verification passed"
    Write-Info "to merge manually: git checkout main; git merge --no-ff $branchName"
    return
}

$shouldMerge = $false
if ($AutoMerge) {
    Write-OK "-AutoMerge set; will merge automatically on green verification"
    $shouldMerge = $true
} else {
    Write-Host ""
    Write-Host "Lint+test green." -ForegroundColor Green
    Write-Host "Did the opencode window show this exact line?" -ForegroundColor Yellow
    Write-Host "    WS_IMPLEMENTATION_COMPLETE: $WsId" -ForegroundColor Green
    $answer = Read-Host "Merge $branchName to main? (y/N)"
    $shouldMerge = ($answer -eq 'y' -or $answer -eq 'Y')
}

if (-not $shouldMerge) {
    Write-Skip "merge declined; branch $branchName left as-is"
    Write-Info "to resume later: .\scripts\Invoke-WorkstreamTui.ps1 -WsId $WsId -Resume"
    return
}

# Commit any uncommitted leftovers, then merge.
# Same `git add -u` rationale as the resume-mode leftover commit above:
# only stage tracked modifications, not untracked junk files.
$uncommitted = Invoke-GitCapture 'status','--porcelain'
if ($uncommitted.Trim()) {
    Write-Step "committing tracked-file modifications"
    Invoke-Git 'add','-u'
    Invoke-GitCapture 'commit','-m',("feat($slug): $WsId leftover from TUI run") | Out-Null
}

Write-Step "merging $branchName to main (--no-ff)"
if (-not (Invoke-Git 'checkout','main')) { throw "couldn't checkout main" }
$mergeMsg = "merge: $WsId $slug"
if ($Resume) { $mergeMsg += " (resumed)" }
$mergeOut = Invoke-GitCapture 'merge','--no-ff',$branchName,'-m',$mergeMsg
Write-Host $mergeOut

if ($LASTEXITCODE -eq 0) {
    Invoke-Git 'branch','-D',$branchName
    Write-OK "$WsId merged to main"
} else {
    Write-Fail "merge failed (conflicts?); leaving branch $branchName"
    Write-Info "resolve manually, then: git merge --continue  (or  git merge --abort)"
}
