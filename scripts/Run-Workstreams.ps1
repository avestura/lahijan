<#
.SYNOPSIS
    Lahijan autonomous workstream runner.

.DESCRIPTION
    Runs one or more workstreams sequentially through opencode's ws-implementer
    subagent. For each WS:
      1. Branches from main as feat/ws-XX-<slug>
      2. Invokes: opencode run --agent ws-implementer --auto --print-logs
      3. Verifies with: make lint test
      4. If green: merges to main (--no-ff), deletes the branch, continues
      5. If red:   leaves the branch for morning review, logs, continues

    Designed to run in a foreground terminal overnight. To halt gracefully
    between WSs, create a STOP file:
        New-Item logs\ws-runner\STOP

.PARAMETER Workstreams
    Array of WS IDs to run, in order. Defaults to the full MVP sequence in
    dependency order (Phase 0..6, skipping Phase 7 deferred).

.PARAMETER Model
    Optional provider/model override, e.g. "anthropic/claude-sonnet-4-6".
    Defaults to whatever opencode is configured to use.

.PARAMETER LogDir
    Where to write per-WS logs. Default: logs/ws-runner

.PARAMETER TimeoutMinutes
    Per-WS hard timeout. If exceeded, the WS is marked 'timeout' and skipped.

.PARAMETER DryRun
    Skip the actual opencode invocation; just print what would happen.

.PARAMETER SkipPreflight
    Skip the pre-flight checks (opencode on PATH, clean tree, etc.).

.EXAMPLE
    .\scripts\Run-Workstreams.ps1
    # Run the full MVP sequence.

.EXAMPLE
    .\scripts\Run-Workstreams.ps1 -Workstreams WS-03,WS-04,WS-05
    # Run just Phase 0.

.EXAMPLE
    .\scripts\Run-Workstreams.ps1 -DryRun
    # Print what would happen, do nothing.
#>

[CmdletBinding()]
param(
    [string[]]$Workstreams,
    [string]$Model,
    [string]$LogDir = "logs/ws-runner",
    [int]$TimeoutMinutes = 90,
    [switch]$DryRun,
    [switch]$SkipPreflight
)

$ErrorActionPreference = "Stop"
$ProgressPreference    = "SilentlyContinue"   # opencode emits progress chars

# Default MVP sequence in dependency order.
if (-not $Workstreams) {
    $Workstreams = @(
        'WS-03','WS-04','WS-05',
        'WS-06','WS-08',
        'WS-07a','WS-07b','WS-07c',
        'WS-09',
        'WS-10a','WS-10b','WS-10c',
        'WS-11','WS-12','WS-13',
        'WS-14','WS-15','WS-16','WS-17',
        'WS-18','WS-19','WS-20','WS-21',
        'WS-22','WS-23'
    )
}

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

function Write-Header  { param($Text) Write-Host ""; Write-Host ("=" * 70) -ForegroundColor DarkCyan; Write-Host $Text -ForegroundColor Cyan; Write-Host ("=" * 70) -ForegroundColor DarkCyan }
function Write-Step    { param($Text) Write-Host ">>> $Text" -ForegroundColor Yellow }
function Write-OK      { param($Text) Write-Host "[OK]   $Text" -ForegroundColor Green }
function Write-Skip    { param($Text) Write-Host "[SKIP] $Text" -ForegroundColor DarkYellow }
function Write-Fail    { param($Text) Write-Host "[FAIL] $Text" -ForegroundColor Red }
function Write-Info    { param($Text) Write-Host "       $Text" -ForegroundColor DarkGray }

function Invoke-Git    { param([string[]]$ArgList) & git @ArgList 2>&1 | Out-Null; $LASTEXITCODE -eq 0 }
function Get-GitOutput { param([string[]]$ArgList)
    $prev = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
    $out = & git @ArgList 2>&1
    $ErrorActionPreference = $prev
    return $out
}

function Get-WsSlug {
    param([string]$WsId)
    $files = Get-ChildItem -Path "docs/workstreams" -Filter "$WsId-*.md" -ErrorAction SilentlyContinue
    if (-not $files -or $files.Count -eq 0) { return "impl" }
    $name = $files[0].BaseName   # e.g. "WS-03-database-persistence-core"
    $prefix = "$WsId-"
    if ($name.StartsWith($prefix)) { return $name.Substring($prefix.Length) }
    return "impl"
}

function Test-StopRequested {
    $stopFile = Join-Path $LogDir "STOP"
    (Test-Path $stopFile)
}

function Invoke-Preflight {
    Write-Header "Pre-flight checks"

    if (-not (Get-Command opencode -ErrorAction SilentlyContinue)) {
        throw "opencode is not on PATH. Install it before running."
    }
    Write-OK "opencode on PATH"

    $gopath = go env GOPATH
    $lintPath = Join-Path $gopath "bin"
    if (-not ($env:Path -split ';' -contains $lintPath)) {
        $env:Path = "$lintPath;$env:Path"
        Write-Info "added $lintPath to PATH for this session"
    }
    if (-not (Get-Command golangci-lint -ErrorAction SilentlyContinue)) {
        throw "golangci-lint not on PATH. Run: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.0.2"
    }
    $lintVer = (golangci-lint version 2>&1 | Select-String 'version v' | Out-String).Trim()
    Write-OK "golangci-lint: $lintVer"

    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        Write-Skip "docker not on PATH; some integration tests may fail"
    } else {
        # Docker writes warnings to stderr; suppress those and just check the exit.
        $prev = $ErrorActionPreference; $ErrorActionPreference = 'SilentlyContinue'
        docker info 2>$null | Out-Null
        $dockerOk = ($LASTEXITCODE -eq 0)
        $ErrorActionPreference = $prev
        if (-not $dockerOk) {
            Write-Skip "Docker daemon not running; some integration tests may fail. Start Docker Desktop."
        } else {
            Write-OK "docker daemon running"
        }
    }

    $dirty = Get-GitOutput 'status','--porcelain'
    if ($dirty) {
        Write-Host $dirty
        throw "working tree not clean. Commit or stash before running."
    }
    Write-OK "working tree clean"

    $branch = (Get-GitOutput 'rev-parse','--abbrev-ref','HEAD').Trim()
    if ($branch -ne "main") {
        throw "on branch '$branch'; switch to main first."
    }
    Write-OK "on main"

    # Smoke-test lint+test to confirm baseline is green before agents start.
    Write-Step "verifying baseline (make lint test)..."
    $prev = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
    & make lint test 2>&1 | Out-Host
    $verifyExit = $LASTEXITCODE
    $ErrorActionPreference = $prev
    if ($verifyExit -ne 0) {
        throw "baseline 'make lint test' is red (exit $verifyExit). Fix before running agents."
    }
    Write-OK "baseline lint+test green"
}

function Invoke-Ws {
    param([string]$WsId)

    $slug       = Get-WsSlug $WsId
    $branchName = "feat/$($WsId.ToLower())-$slug"
    $wsDocPath  = "docs/workstreams/$WsId-$slug.md"
    $timestamp  = Get-Date -Format "yyyyMMdd-HHmmss"
    $logFile    = Join-Path $LogDir "$WsId-$timestamp.log"

    Write-Header "$WsId - $slug"
    Write-Info "branch will be: $branchName"
    Write-Info "log file:       $logFile"
    Write-Info "ws doc:         $wsDocPath"

    if (-not (Test-Path $wsDocPath)) {
        Write-Fail "WS doc not found: $wsDocPath"
        return [pscustomobject]@{ WS = $WsId; Result = 'no-doc'; Branch = '(none)'; Log = '(none)' }
    }

    if (Test-StopRequested) {
        Write-Skip "STOP file present at $(Join-Path $LogDir 'STOP'); halting"
        return [pscustomobject]@{ WS = $WsId; Result = 'stopped'; Branch = '(none)'; Log = $logFile }
    }

    if ($DryRun) {
        Write-Skip "DRY RUN"
        return [pscustomobject]@{ WS = $WsId; Result = 'dry-run'; Branch = $branchName; Log = $logFile }
    }

    New-Item -ItemType Directory -Path $LogDir -Force | Out-Null
    "[$(Get-Date)] Starting $WsId ($slug)`n" | Out-File -FilePath $logFile -Encoding utf8

    # Make sure we're on main and clean before branching.
    if (-not (Invoke-Git 'checkout','main')) { throw "couldn't checkout main" }
    if (-not (Invoke-Git 'reset','--hard','HEAD')) { throw "couldn't reset main" }

    # Delete the branch if it somehow exists from a prior failed run.
    & git branch -D $branchName 2>&1 | Out-Null
    if (-not (Invoke-Git 'checkout','-b',$branchName)) {
        Write-Fail "couldn't create branch $branchName"
        return [pscustomobject]@{ WS = $WsId; Result = 'branch-failed'; Branch = $branchName; Log = $logFile }
    }
    Write-OK "on branch $branchName"

    # Build the agent prompt. The template lives in a sibling text file so
    # PowerShell doesn't have to parse the prompt as code.
    $templatePath = Join-Path $PSScriptRoot "_ws-prompt.template"
    $promptTemplate = Get-Content -Path $templatePath -Raw -Encoding utf8
    $prompt = $promptTemplate `
        -replace '\{\{WS_ID\}\}',       $WsId `
        -replace '\{\{SLUG\}\}',        $slug `
        -replace '\{\{REPO_ROOT\}\}',   (Get-Location).Path `
        -replace '\{\{BRANCH\}\}',      $branchName `
        -replace '\{\{WS_DOC_PATH\}\}', $wsDocPath

    $prompt | Out-File -FilePath "$logFile.prompt" -Encoding utf8

    # -----------------------------------------------------------------------
    # Invoke opencode. Pipe output to host AND to the log file.
    # -----------------------------------------------------------------------
    $opencodeArgs = @('run','--agent','ws-implementer','--auto','--print-logs')
    if ($Model) { $opencodeArgs += @('-m', $Model) }
    $opencodeArgs += $prompt

    Write-Step "opencode run --agent ws-implementer (timeout ${TimeoutMinutes}m)"

    # Run opencode in a background job with a hard timeout. Capture stdout+stderr
    # to the log file. We relax ErrorActionPreference so native-stderr output
    # doesn't trip Stop-mode.
    $job = Start-Job -ScriptBlock {
        param($Exe, $ArgArray, $Log)
        $ErrorActionPreference = 'Continue'
        & $Exe @ArgArray 2>&1 | Tee-Object -FilePath $Log
    } -ArgumentList 'opencode', $opencodeArgs, $logFile

    $finished = $job | Wait-Job -Timeout ($TimeoutMinutes * 60)
    if (-not $finished) {
        $job | Stop-Job
        Write-Fail "WS $WsId exceeded ${TimeoutMinutes}m timeout"
        Receive-Job $job 2>&1 | Out-Host
        $job | Remove-Job
        if (-not (Invoke-Git 'checkout','main')) { Write-Warning "couldn't return to main" }
        return [pscustomobject]@{ WS = $WsId; Result = 'timeout'; Branch = $branchName; Log = $logFile }
    }
    Receive-Job $job 2>&1 | Out-Host
    $job | Remove-Job

    # -----------------------------------------------------------------------
    # Verify with make lint test
    # -----------------------------------------------------------------------
    Write-Step "verifying: make lint test"
    $prev = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
    & make lint test 2>&1 | Tee-Object -FilePath "$logFile.verify" | Out-Host
    $verifyExit = $LASTEXITCODE
    $ErrorActionPreference = $prev

    # Check whether the agent printed the completion marker.
    $logContent = if (Test-Path $logFile) { Get-Content $logFile -Raw } else { '' }
    $marker = if ($logContent -match "WS_IMPLEMENTATION_COMPLETE:\s*$WsId") {
        'complete'
    } elseif ($logContent -match "WS_IMPLEMENTATION_BLOCKED:\s*$WsId") {
        'blocked'
    } else {
        'no-marker'
    }

    Write-Info "agent marker: $marker"
    Write-Info "verify exit:  $verifyExit"

    if ($verifyExit -eq 0 -and $marker -eq 'complete') {
        # -------------------------------------------------------------------
        # Success: commit any stragglers, merge to main, delete branch
        # -------------------------------------------------------------------
        $uncommitted = Get-GitOutput 'status','--porcelain'
        if ($uncommitted) {
            Write-Step "committing leftover uncommitted work"
            & git add -A
            & git commit -m "feat($slug): $WsId leftover artifacts from agent run" 2>&1 | Out-File -FilePath $logFile -Append -Encoding utf8
        }

        Write-Step "merging $branchName to main (--no-ff)"
        if (-not (Invoke-Git 'checkout','main')) { throw "couldn't checkout main" }
        $mergeMsg = "merge: $WsId $slug"
        & git merge --no-ff $branchName -m $mergeMsg 2>&1 |
            Tee-Object -FilePath $logFile -Append | Out-Host

        if ($LASTEXITCODE -eq 0) {
            & git branch -D $branchName 2>&1 | Out-Null
            Write-OK "$WsId merged to main"
            return [pscustomobject]@{ WS = $WsId; Result = 'ok'; Branch = '(merged)'; Log = $logFile }
        } else {
            Write-Fail "merge failed; leaving branch $branchName"
            return [pscustomobject]@{ WS = $WsId; Result = 'merge-failed'; Branch = $branchName; Log = $logFile }
        }
    } else {
        # -------------------------------------------------------------------
        # Failure: leave the branch for morning review
        # -------------------------------------------------------------------
        Write-Fail "$WsId verification failed (marker=$marker, verify_exit=$verifyExit)"
        Write-Info "branch $branchName left for review"
        Write-Info "log:   $logFile"
        if (-not (Invoke-Git 'checkout','main')) {
            Write-Warning "couldn't return to main; you may need to 'git checkout main' manually"
        }
        return [pscustomobject]@{ WS = $WsId; Result = 'failed'; Branch = $branchName; Log = $logFile }
    }
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

if (-not $SkipPreflight) {
    Invoke-Preflight
}

Write-Header "Running $($Workstreams.Count) workstream(s): $($Workstreams -join ', ')"
Write-Info "started at: $(Get-Date -Format 'o')"
Write-Info "log dir:    $LogDir"
Write-Info "dry run:    $DryRun"
Write-Info "timeout:    ${TimeoutMinutes}m per WS"

$results = @()
$startTime = Get-Date

foreach ($ws in $Workstreams) {
    try {
        $r = Invoke-Ws -WsId $ws
        $results += $r
    } catch {
        Write-Fail "exception in $ws : $_"
        $results += [pscustomobject]@{ WS = $ws; Result = 'exception'; Branch = '(unknown)'; Log = '(none)' }
        # Try to return to main so the next WS can branch cleanly.
        Invoke-Git 'checkout','main' | Out-Null
    }

    # Persist running summary after each WS so we can peek mid-run.
    $results | ConvertTo-Json -Depth 4 |
        Out-File -FilePath (Join-Path $LogDir "summary.json") -Encoding utf8
}

$elapsed = (Get-Date) - $startTime

# ---------------------------------------------------------------------------
# Final summary
# ---------------------------------------------------------------------------

Write-Header "Summary"
Write-Info "elapsed: $($elapsed.ToString())"
Write-Info "finished at: $(Get-Date -Format 'o')"
$results | Format-Table -AutoSize

$ok     = ($results | Where-Object Result -eq 'ok').Count
$failed = ($results | Where-Object { $_.Result -in @('failed','timeout','merge-failed','exception') }).Count
$other  = $results.Count - $ok - $failed

Write-Host ""
Write-Host "  ok: $ok    failed: $failed    other: $other    total: $($results.Count)"
Write-Host ""
Write-Host "Logs in:    $LogDir"
Write-Host "Summary:    $(Join-Path $LogDir 'summary.json')"
Write-Host ""

if ($failed -gt 0) {
    Write-Host "Failed branches left for review:" -ForegroundColor Red
    $results | Where-Object { $_.Result -in @('failed','timeout','merge-failed','exception') -and $_.Branch -ne '(none)' -and $_.Branch -ne '(merged)' } |
        ForEach-Object { Write-Host "  $($_.WS) -> $($_.Branch)" -ForegroundColor Red }
    Write-Host ""
    Write-Host "Inspect a failed WS:" -ForegroundColor Yellow
    Write-Host "  git checkout <branch>"
    Write-Host "  git log"
    Write-Host "  type logs\ws-runner\<WS>-<timestamp>.log"
}

Write-Host ""
Write-Host "Done." -ForegroundColor Green
