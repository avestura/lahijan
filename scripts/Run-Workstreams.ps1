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
    Default is 500 minutes (~8.3 hours) - generous, because a complex WS can
    legitimately take a couple of hours of agent work plus compose stack
    bring-up + integration tests. Set lower with -TimeoutMinutes 120 if you
    want to fail-fast.

.PARAMETER DryRun
    Skip the actual opencode invocation; just print what would happen.

.PARAMETER SkipPreflight
    Skip the pre-flight checks (opencode on PATH, clean tree, etc.).

.PARAMETER LiveStream
    Live-stream opencode's stdout to the console while it runs (default: ON).
    Lets you watch what the agent is doing instead of staring at a blank
    'opencode run --agent ws-implementer (timeout 90m)' line for an hour.
    Pass -LiveStream:$false to disable (falls back to tail-after-exit).

.PARAMETER StreamStderr
    Also live-stream opencode's stderr to the console (in yellow). Default OFF
    because opencode's --print-logs is noisy; turn on if you want the firehose.

.EXAMPLE
    .\scripts\Run-Workstreams.ps1
    # Run the full MVP sequence with live streaming.

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
    [int]$TimeoutMinutes = 500,
    [switch]$DryRun,
    [switch]$SkipPreflight,
    [switch]$LiveStream = $true,
    [switch]$StreamStderr = $false
)

$ErrorActionPreference = "Stop"
$ProgressPreference    = "SilentlyContinue"   # opencode emits progress chars

# Default MVP sequence in dependency order.
if (-not $Workstreams) {
    $Workstreams = @(
        'WS-05',
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

# Invoke-Git runs git with stderr suppressed and returns $true on exit 0.
# Crucially, it temporarily relaxes $ErrorActionPreference so that native-command
# stderr output (like git's "Already on 'main'" or warnings) doesn't get
# promoted to a terminating error under Stop mode.
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

# Invoke-GitCapture runs git, captures combined stdout+stderr, and returns
# the output as a single string. Exit code is in $LASTEXITCODE.
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

# Kept as an alias for backward-compat with anything that calls Get-GitOutput.
function Get-GitOutput { param([string[]]$ArgList) Invoke-GitCapture -ArgList $ArgList }

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
    # (exits non-zero if branch absent; that's fine - capture to suppress leak)
    $null = Invoke-Git 'branch','-D',$branchName
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
    # Invoke opencode via Start-Process (NOT Start-Job).
    # Start-Job + Tee-Object does NOT reliably capture native-command output
    # in Windows PowerShell - the log file ends up empty even when opencode
    # is producing lots of output. Start-Process redirects at the OS level,
    # which is reliable.
    # -----------------------------------------------------------------------
    $opencodeArgs = @('run','--agent','ws-implementer','--auto','--print-logs')
    if ($Model) { $opencodeArgs += @('-m', $Model) }
    $opencodeArgs += $prompt

    Write-Step "opencode run --agent ws-implementer (timeout ${TimeoutMinutes}m)"
    Write-Info "log: $logFile"
    Write-Info "stderr -> $logFile.err"

    # Start opencode with stdout+stderr redirected to files. PassThru gives us
    # the Process object so we can WaitForExit with a timeout.
    $startProcessArgs = @{
        FilePath               = 'opencode'
        ArgumentList           = $opencodeArgs
        RedirectStandardOutput = $logFile
        RedirectStandardError  = "$logFile.err"
        NoNewWindow            = $true
        PassThru               = $true
    }

    $prev = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
    try {
        $proc = Start-Process @startProcessArgs
    } catch {
        $ErrorActionPreference = $prev
        Write-Fail "couldn't start opencode: $($_.Exception.Message)"
        if (-not (Invoke-Git 'checkout','main')) { Write-Warning "couldn't return to main" }
        return [pscustomobject]@{ WS = $WsId; Result = 'spawn-failed'; Branch = $branchName; Log = $logFile }
    }

    # ---------------------------------------------------------------------
    # Wait for opencode to finish, with two modes:
    #   - LiveStream (default): live-stream stdout (and optionally stderr)
    #     to the console while we wait. Uses a shared-read FileStream so we
    #     can read what opencode is writing in real time.
    #   - No LiveStream: plain WaitForExit with timeout; print tail after.
    # In both modes the timeout is enforced.
    # ---------------------------------------------------------------------
    $exitCode  = -1
    $timedOut  = $false
    $deadline  = (Get-Date).AddMinutes($TimeoutMinutes)

    if ($LiveStream) {
        Write-Step "streaming live output (timeout at $($deadline.ToString('HH:mm:ss')))"
        Write-Host "--- opencode output (live) ---" -ForegroundColor DarkGray

        # Wait briefly for the log file to appear (Start-Process redirect
        # creates it lazily on first write).
        $fileWaitEnd = (Get-Date).AddSeconds(30)
        while (-not $proc.HasExited -and -not (Test-Path $logFile) -and (Get-Date) -lt $fileWaitEnd) {
            Start-Sleep -Milliseconds 200
        }

        # Open one reader for stdout (and optionally stderr) with shared read
        # access so opencode can keep appending.
        $outReader = $null
        $outStream = $null
        $errReader = $null
        $errStream = $null
        if (Test-Path $logFile) {
            try {
                $outStream = [System.IO.File]::Open(
                    $logFile,
                    [System.IO.FileMode]::Open,
                    [System.IO.FileAccess]::Read,
                    [System.IO.FileShare]::ReadWrite
                )
                $outReader = New-Object System.IO.StreamReader($outStream)
            } catch {
                Write-Warning "couldn't open $logFile for streaming: $($_.Exception.Message)"
            }
        }
        if ($StreamStderr -and (Test-Path "$logFile.err")) {
            try {
                $errStream = [System.IO.File]::Open(
                    "$logFile.err",
                    [System.IO.FileMode]::Open,
                    [System.IO.FileAccess]::Read,
                    [System.IO.FileShare]::ReadWrite
                )
                $errReader = New-Object System.IO.StreamReader($errStream)
            } catch { }
        }

        # Polling loop: drain new lines from the file, check timeout.
        while (-not $proc.HasExited) {
            if ($outReader) {
                while (-not $outReader.EndOfStream) {
                    $line = $outReader.ReadLine()
                    if ($null -ne $line) { Write-Host $line -ForegroundColor DarkGray }
                }
            }
            if ($errReader) {
                while (-not $errReader.EndOfStream) {
                    $line = $errReader.ReadLine()
                    if ($null -ne $line) { Write-Host $line -ForegroundColor DarkYellow }
                }
            }
            if ((Get-Date) -gt $deadline) {
                # taskkill /T /F walks the process tree and kills all descendants.
                # opencode spawns a long-lived server subprocess; $proc.Kill() only
                # terminates the parent and leaves the orphan server running for
                # hours (observed in the WS-19 2026-07-19 incident: the orphan
                # kept writing to stderr for 13+ hours after the parent was killed).
                try { & taskkill /PID $proc.Id /T /F 2>&1 | Out-Null } catch { Write-Warning "couldn't kill opencode tree: $($_.Exception.Message)" }
                $timedOut = $true
                Write-Fail "WS $WsId exceeded ${TimeoutMinutes}m timeout"
                break
            }
            Start-Sleep -Milliseconds 100
        }

        # Final drain (anything written between last poll and process exit).
        if ($outReader) {
            while (-not $outReader.EndOfStream) {
                $line = $outReader.ReadLine()
                if ($null -ne $line) { Write-Host $line -ForegroundColor DarkGray }
            }
            $outReader.Dispose()
            $outStream.Dispose()
        }
        if ($errReader) {
            while (-not $errReader.EndOfStream) {
                $line = $errReader.ReadLine()
                if ($null -ne $line) { Write-Host $line -ForegroundColor DarkYellow }
            }
            $errReader.Dispose()
            $errStream.Dispose()
        }

        Write-Host "--- end of opencode output ---" -ForegroundColor DarkGray
    } else {
        # Plain wait mode: block until exit or timeout, then print tail.
        $exited = $proc.WaitForExit($TimeoutMinutes * 60 * 1000)
        if (-not $exited) {
            # taskkill /T /F walks the process tree (see LiveStream branch above
            # for the rationale: $proc.Kill() leaves opencode's server orphaned).
            try { & taskkill /PID $proc.Id /T /F 2>&1 | Out-Null } catch { Write-Warning "couldn't kill opencode tree" }
            $timedOut = $true
            Write-Fail "WS $WsId exceeded ${TimeoutMinutes}m timeout"
        }
        if (Test-Path $logFile) {
            Write-Host "--- opencode output (tail) ---" -ForegroundColor DarkGray
            Get-Content $logFile -Tail 30 | ForEach-Object { Write-Host $_ -ForegroundColor DarkGray }
        }
    }

    $ErrorActionPreference = $prev

    if (-not $timedOut -and $proc.HasExited) {
        $exitCode = $proc.ExitCode
        Write-OK "opencode exited (code $exitCode)"
    }

    # -----------------------------------------------------------------------
    # Verify with make lint test
    # -----------------------------------------------------------------------
    Write-Step "verifying: make lint test"
    $prev = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
    & make lint test 2>&1 | Tee-Object -FilePath "$logFile.verify" | Out-Host
    $verifyExit = $LASTEXITCODE
    $ErrorActionPreference = $prev

    # Check whether the agent printed the completion marker. Look in both
    # stdout and stderr captures.
    $logContent = ''
    if (Test-Path $logFile)        { $logContent += Get-Content $logFile -Raw }
    if (Test-Path "$logFile.err")  { $logContent += "`n" + (Get-Content "$logFile.err" -Raw) }
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
        $uncommitted = Invoke-GitCapture 'status','--porcelain'
        if ($uncommitted.Trim()) {
            Write-Step "committing leftover uncommitted work"
            Invoke-Git 'add','-A'
            $commitOut = Invoke-GitCapture 'commit','-m',("feat($slug): $WsId leftover artifacts from agent run")
            $commitOut | Out-File -FilePath $logFile -Append -Encoding utf8
        }

        Write-Step "merging $branchName to main (--no-ff)"
        if (-not (Invoke-Git 'checkout','main')) { throw "couldn't checkout main" }
        $mergeMsg = "merge: $WsId $slug"
        $mergeOut = Invoke-GitCapture 'merge','--no-ff',$branchName,'-m',$mergeMsg
        $mergeOut | Out-File -FilePath $logFile -Append -Encoding utf8
        $mergeOk = ($LASTEXITCODE -eq 0)
        Write-Host $mergeOut

        if ($mergeOk) {
            Invoke-Git 'branch','-D',$branchName
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

# Make sure the log dir exists even if the first WS fails before creating it.
New-Item -ItemType Directory -Path $LogDir -Force | Out-Null

$results = @()
$startTime = Get-Date

foreach ($ws in $Workstreams) {
    try {
        $r = Invoke-Ws -WsId $ws
        $results += $r
    } catch {
        Write-Fail "exception in ${ws}: $($_.Exception.Message)"
        $results += [pscustomobject]@{ WS = $ws; Result = 'exception'; Branch = '(unknown)'; Log = '(none)' }

        # Best-effort recovery: figure out where we are, preserve any uncommitted
        # agent work on its branch, then return to main so the next WS can branch.
        $prev = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
        try {
            $currentBranch = (& git rev-parse --abbrev-ref HEAD 2>&1).ToString().Trim()
            if ($currentBranch -ne "main") {
                # We're on a WS branch; commit any pending work so it survives.
                & git add -A 2>&1 | Out-Null
                & git commit -m "wip: $ws exception state (auto-saved by runner)" --no-verify 2>&1 | Out-Null
                Write-Info "agent work saved on branch $currentBranch"
            }
            # Discard any untracked files on main so the next WS starts clean.
            & git checkout main 2>&1 | Out-Null
            & git clean -fd 2>&1 | Out-Null
        } catch {
            Write-Warning "recovery failed: $($_.Exception.Message)"
            Write-Warning "you may need to manually: git checkout main; git reset --hard"
        } finally {
            $ErrorActionPreference = $prev
        }
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
