# Lahijan - stop the dev environment.
#
# Stops the Go backend + every frontend that scripts/Run-Dev.ps1 launched,
# by reading the PID files in dist\dev-pids\. Leaves the Docker compose
# stack running by default (pass -IncludeContainers to tear that down too).
#
# Usage:
#   .\scripts\Stop-Dev.ps1                          # stop backend + frontends
#   .\scripts\Stop-Dev.ps1 -IncludeContainers       # also stop compose stack
#   .\scripts\Stop-Dev.ps1 -IncludeContainers -PurgeVolumes   # full wipe

[CmdletBinding()]
param(
    [switch]$IncludeContainers,
    [switch]$PurgeVolumes
)

$ErrorActionPreference = "Continue"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot  = Split-Path -Parent $ScriptDir
Set-Location $RepoRoot

$PidDir    = Join-Path $RepoRoot "dist\dev-pids"
$Compose   = Join-Path $RepoRoot "deployments\docker-compose.dev.yml"
$EnvFile   = Join-Path $RepoRoot "deployments\.env"

function Stop-PidFile([string]$pidFile, [string]$label) {
    if (-not (Test-Path $pidFile)) {
        Write-Host "    $label : no PID file, skipping" -ForegroundColor DarkGray
        return
    }
    $pidVal = (Get-Content $pidFile -ErrorAction SilentlyContinue | Select-Object -First 1)
    if ($pidVal -match '^\d+$') {
        # /T = kill child tree (cmd.exe -> npm -> node), /F = force.
        & taskkill /F /T /PID ([int]$pidVal) 2>$null | Out-Null
        Write-Host "    $label : killed tree rooted at PID $pidVal" -ForegroundColor Green
    }
    Remove-Item $pidFile -ErrorAction SilentlyContinue
}

function Stop-Port([int]$port, [string]$label) {
    $conns = Get-NetTCPConnection -State Listen -LocalPort $port -ErrorAction SilentlyContinue
    foreach ($c in $conns) {
        if ($c.OwningProcess -and $c.OwningProcess -ne 0) {
            & taskkill /F /T /PID $c.OwningProcess 2>$null | Out-Null
            Write-Host "    $label : killed stray listener PID $($c.OwningProcess) on port $port" -ForegroundColor Yellow
        }
    }
}

Write-Host "==> Stopping Lahijan dev processes" -ForegroundColor Cyan

Stop-PidFile (Join-Path $PidDir "backend.pid")  "Backend       "
Stop-PidFile (Join-Path $PidDir "frontend.pid") "Dashboard     "
Stop-PidFile (Join-Path $PidDir "website.pid")  "Marketing site"
Stop-PidFile (Join-Path $PidDir "docs.pid")     "Docs site     "

# Belt-and-braces: kill anything still listening on the dev ports
# (catches processes that escaped the PID file, e.g. crashed mid-launch).
Stop-Port 8080 "Backend       "
Stop-Port 5173 "Dashboard     "
Stop-Port 5174 "Marketing site"
Stop-Port 3000 "Docs site     "

# Also kill any stray lahijan.exe processes that escaped the PID file
# (e.g. crash before PID write).
Get-Process lahijan -ErrorAction SilentlyContinue | ForEach-Object {
    Write-Host "    Killing stray backend PID $($_.Id)" -ForegroundColor Yellow
    Stop-Process -Id $_.Id -Force -ErrorAction SilentlyContinue
}

if ($IncludeContainers) {
    Write-Host "==> Stopping Docker compose stack" -ForegroundColor Cyan
    $downArgs = @("compose", "-f", $Compose, "--env-file", $EnvFile, "down")
    if ($PurgeVolumes) { $downArgs += "-v" }
    $downArgs += "--remove-orphans"
    & docker @downArgs
    if ($PurgeVolumes) {
        Write-Host "    Volumes wiped" -ForegroundColor Green
    } else {
        Write-Host "    Stack stopped (volumes preserved)" -ForegroundColor Green
    }
}

Write-Host "==> Done" -ForegroundColor Green
