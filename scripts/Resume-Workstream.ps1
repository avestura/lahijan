<#
.SYNOPSIS
    Resume an in-progress workstream on its existing branch.

.DESCRIPTION
    For when Run-Workstreams.ps1 timed out or failed mid-WS but the branch
    has real committed work you don't want to throw away. Resume-Workstream.ps1:

      1. Verifies the branch feat/ws-XX-<slug> exists and is ahead of main
      2. Checks it out (does NOT reset, does NOT delete)
      3. Invokes opencode --agent ws-implementer with a resume-specific prompt
         that tells the agent: read AGENTS.md + the WS doc + the existing
         commits on this branch, then pick up where it left off.
      4. Live-streams output to the console (same streaming code as the runner)
      5. Verifies with: make lint test
      6. If green AND agent printed WS_IMPLEMENTATION_COMPLETE: commits any
         leftovers and merges to main (--no-ff), like the runner.
      7. If red or timed out: leaves the branch as-is for further work.

.PARAMETER WsId
    The workstream ID to resume, e.g. WS-04. Required.

.PARAMETER Model
    Optional provider/model override, e.g. "anthropic/claude-sonnet-4-6".

.PARAMETER LogDir
    Where to write the log. Default: logs/ws-runner

.PARAMETER TimeoutMinutes
    Per-resume hard timeout. Default: 500 (same as the runner).

.PARAMETER StreamStderr
    Also live-stream opencode's stderr to the console (in yellow).

.EXAMPLE
    .\scripts\Resume-Workstream.ps1 WS-04
    # Pick up WS-04 where it left off.

.EXAMPLE
    .\scripts\Resume-Workstream.ps1 WS-07c -TimeoutMinutes 240
    # Resume WS-07c with a shorter 4-hour budget.
#>

[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)]
    [string]$WsId,
    [string]$Model,
    [string]$LogDir = "logs/ws-runner",
    [int]$TimeoutMinutes = 500,
    [switch]$StreamStderr
)

$ErrorActionPreference = "Stop"
$ProgressPreference    = "SilentlyContinue"

# ---------------------------------------------------------------------------
# Helpers (mirror Run-Workstreams.ps1)
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

Write-Header "Resume $WsId - pre-flight"

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
    throw "golangci-lint not on PATH."
}
Write-OK "golangci-lint on PATH"

$slug       = Get-WsSlug $WsId
$branchName = "feat/$($WsId.ToLower())-$slug"
$wsDocPath  = "docs/workstreams/$WsId-$slug.md"

# Verify branch exists
$branchList = Invoke-GitCapture 'branch','--list',$branchName
if (-not $branchList.Trim()) {
    throw "branch $branchName does not exist. Nothing to resume. Use Run-Workstreams.ps1 to start $WsId from scratch."
}
Write-OK "branch $branchName exists"

# Check it out (without resetting)
if (-not (Invoke-Git 'checkout',$branchName)) {
    throw "couldn't check out $branchName"
}
Write-OK "on branch $branchName"

# How far ahead of main is it?
$aheadRaw = Invoke-GitCapture 'rev-list','--count',"main..$branchName"
$aheadCount = [int]$aheadRaw.Trim()
if ($aheadCount -eq 0) {
    Write-Skip "branch has no commits ahead of main; resuming anyway (agent will start fresh on this branch)"
} else {
    Write-OK "branch is $aheadCount commit(s) ahead of main"
    Write-Info "recent commits:"
    Invoke-GitCapture 'log','--oneline','-5',"main..$branchName" |
        ForEach-Object { Write-Info "  $_" }
}

# Uncommitted state?
$uncommitted = Invoke-GitCapture 'status','--porcelain'
if ($uncommitted.Trim()) {
    Write-Host ""
    Write-Host "Uncommitted working-tree state on this branch:" -ForegroundColor Yellow
    Write-Host $uncommitted
    Write-Host ""
}

# ---------------------------------------------------------------------------
# Build the resume prompt
# ---------------------------------------------------------------------------

$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
$logFile   = Join-Path $LogDir "$WsId-resume-$timestamp.log"

New-Item -ItemType Directory -Path $LogDir -Force | Out-Null

$templatePath = Join-Path $PSScriptRoot "_ws-resume-prompt.template"
if (-not (Test-Path $templatePath)) {
    throw "missing prompt template: $templatePath"
}
$promptTemplate = Get-Content -Path $templatePath -Raw -Encoding utf8
$rootPath = (Get-Location).Path
$prompt = $promptTemplate `
    -replace '\{\{WS_ID\}\}',       $WsId `
    -replace '\{\{SLUG\}\}',        $slug `
    -replace '\{\{REPO_ROOT\}\}',   $rootPath `
    -replace '\{\{BRANCH\}\}',      $branchName `
    -replace '\{\{WS_DOC_PATH\}\}', $wsDocPath `
    -replace '\{\{AHEAD_COUNT\}\}', $aheadCount

$prompt | Out-File -FilePath "$logFile.prompt" -Encoding utf8

# Commit any uncommitted leftover BEFORE invoking the agent, so the agent's
# first action is in a clean tree and it can see what's already been done.
if ($uncommitted.Trim()) {
    Write-Step "committing uncommitted leftover from prior session"
    Invoke-Git 'add','-A'
    $commitOut = Invoke-GitCapture 'commit','-m',("wip: $WsId leftover from prior timed-out session (auto-committed by Resume-Workstream)")
    Write-Info $commitOut.Trim()
}

# ---------------------------------------------------------------------------
# Invoke opencode with live streaming
# ---------------------------------------------------------------------------

$opencodeArgs = @('run','--agent','ws-implementer','--auto','--print-logs')
if ($Model) { $opencodeArgs += @('-m', $Model) }
$opencodeArgs += $prompt

Write-Header "$WsId - $slug (resume)"
Write-Step "opencode run --agent ws-implementer (timeout ${TimeoutMinutes}m)"
Write-Info "log:    $logFile"
Write-Info "stderr: $logFile.err"
Write-Host ""

$startProcessArgs = @{
    FilePath               = 'opencode'
    ArgumentList           = $opencodeArgs
    RedirectStandardOutput = $logFile
    RedirectStandardError  = "$logFile.err"
    NoNewWindow            = $true
    PassThru               = $true
}

$prev = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
$proc = Start-Process @startProcessArgs
$ErrorActionPreference = $prev

if (-not $proc) {
    throw "couldn't start opencode"
}

$deadline = (Get-Date).AddMinutes($TimeoutMinutes)
Write-Step "streaming live output (timeout at $($deadline.ToString('HH:mm:ss')))"
Write-Host "--- opencode output (live) ---" -ForegroundColor DarkGray

# Wait briefly for the log file to appear
$fileWaitEnd = (Get-Date).AddSeconds(30)
while (-not $proc.HasExited -and -not (Test-Path $logFile) -and (Get-Date) -lt $fileWaitEnd) {
    Start-Sleep -Milliseconds 200
}

$outReader = $null; $outStream = $null
$errReader = $null; $errStream = $null
if (Test-Path $logFile) {
    try {
        $outStream = [System.IO.File]::Open($logFile, [System.IO.FileMode]::Open, [System.IO.FileAccess]::Read, [System.IO.FileShare]::ReadWrite)
        $outReader = New-Object System.IO.StreamReader($outStream)
    } catch { Write-Warning "couldn't open $logFile for streaming: $($_.Exception.Message)" }
}
if ($StreamStderr -and (Test-Path "$logFile.err")) {
    try {
        $errStream = [System.IO.File]::Open("$logFile.err", [System.IO.FileMode]::Open, [System.IO.FileAccess]::Read, [System.IO.FileShare]::ReadWrite)
        $errReader = New-Object System.IO.StreamReader($errStream)
    } catch { }
}

$timedOut = $false
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
        try { $proc.Kill() } catch { Write-Warning "couldn't kill opencode: $($_.Exception.Message)" }
        $timedOut = $true
        Write-Fail "$WsId exceeded ${TimeoutMinutes}m timeout"
        break
    }
    Start-Sleep -Milliseconds 100
}

if ($outReader) {
    while (-not $outReader.EndOfStream) { $line = $outReader.ReadLine(); if ($null -ne $line) { Write-Host $line -ForegroundColor DarkGray } }
    $outReader.Dispose(); $outStream.Dispose()
}
if ($errReader) {
    while (-not $errReader.EndOfStream) { $line = $errReader.ReadLine(); if ($null -ne $line) { Write-Host $line -ForegroundColor DarkYellow } }
    $errReader.Dispose(); $errStream.Dispose()
}

Write-Host "--- end of opencode output ---" -ForegroundColor DarkGray

$exitCode = if ($proc.HasExited) { $proc.ExitCode } else { -1 }
if (-not $timedOut) { Write-OK "opencode exited (code $exitCode)" }

# ---------------------------------------------------------------------------
# Verify with make lint test
# ---------------------------------------------------------------------------

Write-Step "verifying: make lint test"
$prev = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
& make lint test 2>&1 | Tee-Object -FilePath "$logFile.verify" | Out-Host
$verifyExit = $LASTEXITCODE
$ErrorActionPreference = $prev

# Marker search across stdout + stderr
$logContent = ''
if (Test-Path $logFile)        { $logContent += Get-Content $logFile -Raw }
if (Test-Path "$logFile.err")  { $logContent += "`n" + (Get-Content "$logFile.err" -Raw) }
$marker = if ($logContent -match "WS_IMPLEMENTATION_COMPLETE:\s*$WsId") { 'complete' }
          elseif ($logContent -match "WS_IMPLEMENTATION_BLOCKED:\s*$WsId") { 'blocked' }
          else { 'no-marker' }

Write-Info "agent marker: $marker"
Write-Info "verify exit:  $verifyExit"

if ($verifyExit -eq 0 -and $marker -eq 'complete') {
    # Commit any final leftovers, then merge.
    $uncommitted = Invoke-GitCapture 'status','--porcelain'
    if ($uncommitted.Trim()) {
        Write-Step "committing final leftover work"
        Invoke-Git 'add','-A'
        Invoke-GitCapture 'commit','-m',("feat($slug): $WsId final leftover from resume run") | Out-Null
    }

    Write-Step "merging $branchName to main (--no-ff)"
    if (-not (Invoke-Git 'checkout','main')) { throw "couldn't checkout main" }
    $mergeMsg = "merge: $WsId $slug (resumed)"
    $mergeOut = Invoke-GitCapture 'merge','--no-ff',$branchName,'-m',$mergeMsg
    Write-Host $mergeOut
    if ($LASTEXITCODE -eq 0) {
        Invoke-Git 'branch','-D',$branchName
        Write-OK "$WsId merged to main"
    } else {
        Write-Fail "merge failed; leaving branch $branchName"
    }
} else {
    Write-Fail "$WsId verification did not pass (marker=$marker, verify_exit=$verifyExit)"
    Write-Info "branch $branchName left as-is; you can resume again or finish manually"
}
