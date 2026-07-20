# Lahijan - production install script for Windows (WS-23).
#
# Windows hosts CANNOT run Incus (Incus requires a Linux kernel), so this
# script is intended for two use cases:
#
#   1. The operator runs Docker Desktop on Windows + will SSH into a Linux
#      VM / host where Incus lives. The script installs the Lahijan stack
#      on the Windows host's Docker without Incus support. Compute will
#      degrade to "feature disabled". Use this for a quick dev-style
#      validation of the prod topology on a Windows workstation.
#
#   2. The operator runs Docker Desktop on Windows against a remote Docker
#      host (Linux). Set DOCKER_HOST before invoking the script.
#
# For a real production deploy, use scripts/install.sh on the Linux host
# directly. See deployments/README.md -> Windows operators.
#
# Usage (PowerShell, as the operator):
#
#   .\scripts\install.ps1 [-RepoUrl <url>] [-RepoBranch <ref>] [-InstallDir <path>]
#
# Defaults:
#   -RepoUrl      https://github.com/avestura/lahijan.git
#   -RepoBranch   main
#   -InstallDir   $env:USERPROFILE\lahijan  (e.g. C:\Users\you\lahijan)

[CmdletBinding()]
param(
    [string]$RepoUrl = "https://github.com/avestura/lahijan.git",
    [string]$RepoBranch = "main",
    [string]$InstallDir = ""
)

$ErrorActionPreference = "Stop"

# ----------------------------------------------------------------------------
# Helpers
# ----------------------------------------------------------------------------

function Write-Step($msg) { Write-Host "==> $msg" -ForegroundColor Cyan }
function Write-Ok($msg)   { Write-Host "[OK] $msg" -ForegroundColor Green }
function Write-Warn($msg) { Write-Host "[!]  $msg" -ForegroundColor Yellow }
function Write-Die($msg)  {
    Write-Host "[X]  $msg" -ForegroundColor Red
    exit 1
}

# ----------------------------------------------------------------------------
# Defaults
# ----------------------------------------------------------------------------

if (-not $InstallDir) {
    $InstallDir = Join-Path $env:USERPROFILE "lahijan"
}
$EnvFile       = Join-Path $InstallDir "deployments\.env.prod"
$ComposeFile   = Join-Path $InstallDir "deployments\docker-compose.prod.yml"

# ----------------------------------------------------------------------------
# Prerequisites
# ----------------------------------------------------------------------------

Write-Step "Checking prerequisites..."

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Write-Die "docker not found on PATH. Install Docker Desktop: https://www.docker.com/products/docker-desktop/"
}

$dockerVersion = $null
try {
    $dockerVersion = (docker version --format '{{.Server.Version}}' 2>$null)
} catch {
    Write-Die "docker daemon not running. Start Docker Desktop."
}

if (-not (docker compose version 2>$null)) {
    Write-Die "docker compose subcommand not available. Install Docker Desktop with Compose v2 enabled."
}

# Incus is Linux-only.
Write-Warn "Windows cannot run Incus locally; compute features will be disabled."
Write-Warn "Deploy on a Linux VM/host for full functionality (see deployments/README.md)."

# Disk space check
$drive = (Split-Path -Qualifier $InstallDir)
if ($drive) {
    $free = (Get-PSDrive -Name $drive.TrimEnd(':\') -ErrorAction SilentlyContinue).Free
    if ($free -and $free -lt 10GB) {
        Write-Warn "Less than 10 GB free on $drive. Lahijan needs headroom for Postgres + SeaweedFS volumes."
    }
}

Write-Ok "prerequisites met."

# ----------------------------------------------------------------------------
# Clone / refresh the repo
# ----------------------------------------------------------------------------

if (Test-Path (Join-Path $InstallDir ".git")) {
    Write-Step "Updating existing install at $InstallDir..."
    git -C $InstallDir fetch --quiet origin $RepoBranch
    git -C $InstallDir checkout --quiet $RepoBranch
    git -C $InstallDir reset --quiet --hard "origin/$RepoBranch"
} else {
    Write-Step "Cloning Lahijan into $InstallDir..."
    if (Test-Path $InstallDir) {
        if ((Get-ChildItem $InstallDir -Force).Count -gt 0) {
            Write-Die "$InstallDir exists but is not empty. Move it aside."
        }
    } else {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }
    git clone --depth=1 --branch $RepoBranch $RepoUrl $InstallDir
}
Write-Ok ("code at " + (git -C $InstallDir log -1 --format='%h %s' 2>$null) + "")

# ----------------------------------------------------------------------------
# Render .env.prod
# ----------------------------------------------------------------------------

if (-not (Test-Path $EnvFile)) {
    Write-Step "Rendering $EnvFile from template..."
    Copy-Item (Join-Path $InstallDir "deployments\.env.prod.example") $EnvFile -Force

    function Set-EnvVar($Name, $Prompt, $Default = "") {
        $val = ""
        if ($Default) {
            $val = Read-Host "  $Prompt [$Default]"
            if (-not $val) { $val = $Default }
        } else {
            $val = Read-Host "  $Prompt"
        }
        # Replace the first line that starts with Name=
        $content = Get-Content $EnvFile
        $replaced = $false
        for ($i = 0; $i -lt $content.Count; $i++) {
            if ($content[$i] -match "^$Name=") {
                $content[$i] = "$Name=$val"
                $replaced = $true
                break
            }
        }
        if (-not $replaced) {
            $content += "$Name=$val"
        }
        Set-Content -Path $EnvFile -Value $content
    }

    Write-Host ""
    Write-Host "Lahijan first-time setup." -ForegroundColor White
    Write-Host "Answer the prompts; values are written to $EnvFile"
    Write-Host ""

    Set-EnvVar "LAHIJAN_PUBLIC_HOST"         "Public hostname (must resolve to this host)" "app.example.com"
    Set-EnvVar "LAHIJAN_PUBLIC_URL"          "Public URL (scheme + host)"                  "https://app.example.com"
    Set-EnvVar "LAHIJAN_ACME_EMAIL"          "Email for Let's Encrypt expiry notices"
    Set-EnvVar "LAHIJAN_IMAGE_TAG"           "Lahijan image tag (SemVer)"                  "v0.1.0"
    Set-EnvVar "LAHIJAN_BOOTSTRAP_ADMIN_EMAIL" "First-run platform.admin email"          "admin@example.com"
    Set-EnvVar "POSTGRES_SUPERUSER_PASSWORD" "Postgres superuser password (32+ chars)"
    Set-EnvVar "LAHIJAN_DATABASE_PASSWORD"   "Lahijan DB password (32+ chars)"
    Set-EnvVar "PDNS_DATABASE_PASSWORD"      "PowerDNS DB password (32+ chars)"
    Set-EnvVar "SEAWEED_DATABASE_PASSWORD"   "SeaweedFS DB password (32+ chars)"
    Set-EnvVar "PDNS_API_KEY"                "PowerDNS API key (32 hex chars)"
    Set-EnvVar "SEAWEEDFS_S3_ACCESS_KEY"     "SeaweedFS admin access key (16 hex chars)"
    Set-EnvVar "SEAWEEDFS_S3_SECRET_KEY"     "SeaweedFS admin secret key (32 hex chars)"
    Set-EnvVar "LAHIJAN_AUTH_SIGNING_KEY"    "Auth HMAC signing key (48+ chars)"
    Set-EnvVar "LAHIJAN_AUTH_SECRETS_ENCRYPTIONKEY" "Auth AES-256-GCM key (base64 32 bytes)"
    Set-EnvVar "LAHIJAN_SMTP_HOST"           "SMTP host"
    Set-EnvVar "LAHIJAN_SMTP_USER"           "SMTP user"
    Set-EnvVar "LAHIJAN_SMTP_PASSWORD"       "SMTP password"
    Set-EnvVar "GRAFANA_ADMIN_PASSWORD"      "Grafana admin password"

    Write-Ok "$EnvFile rendered."
} else {
    Write-Ok "$EnvFile already exists; leaving values in place."
}

# ----------------------------------------------------------------------------
# Bring up the stack
# ----------------------------------------------------------------------------

Write-Step "Pulling images..."
docker compose --env-file $EnvFile -f $ComposeFile pull --quiet
if ($LASTEXITCODE -ne 0) { Write-Die "docker pull failed." }

Write-Step "Bringing up the stack (this can take a few minutes on first boot)..."
docker compose --env-file $EnvFile -f $ComposeFile up -d
if ($LASTEXITCODE -ne 0) { Write-Die "docker compose up failed." }

# ----------------------------------------------------------------------------
# Wait for the Lahijan healthcheck to flip green
# ----------------------------------------------------------------------------

Write-Step "Waiting for Lahijan to become healthy..."
$attempt = 0
$maxAttempts = 60
while ($attempt -lt $maxAttempts) {
    $status = (docker inspect --format='{{json .State.Health.Status}}' lahijan-prod-app 2>$null)
    if ($status -match '"healthy"') { break }
    $attempt++
    Write-Host -NoNewline "."
    Start-Sleep -Seconds 5
}
Write-Host ""
if ($attempt -ge $maxAttempts) {
    Write-Warn "Lahijan did not become healthy within $($maxAttempts * 5) seconds."
    Write-Warn "Inspect logs: docker compose --env-file $EnvFile -f $ComposeFile logs lahijan"
    exit 1
}
Write-Ok "Lahijan is healthy."

# ----------------------------------------------------------------------------
# Print first-run admin credentials (WS-23 bootstrap).
# ----------------------------------------------------------------------------

Write-Step "Looking for first-run admin credentials in the Lahijan log..."
Start-Sleep -Seconds 2
$credsLine = (docker compose --env-file $EnvFile -f $ComposeFile logs lahijan 2>$null `
    | Select-String 'first-run admin credentials' `
    | Select-Object -Last 1).Line

$publicUrl = if ($LAHIJAN_PUBLIC_URL) { $LAHIJAN_PUBLIC_URL } else { "https://app.example.com" }

if ($credsLine) {
    Write-Host ""
    Write-Host "Lahijan is live." -ForegroundColor White
    Write-Host ""
    Write-Host "First-run admin credentials (rotate immediately):" -ForegroundColor White
    Write-Host $credsLine
    Write-Host ""
    Write-Host "Dashboard: $publicUrl"
    Write-Host "Grafana:   $publicUrl/grafana"
    Write-Host ""
    Write-Warn "The password above is shown only once. Rotate it via the dashboard after first login."
    Write-Warn "See deployments/SECRETS.md for the rotation procedure."
} else {
    Write-Host ""
    Write-Host "Lahijan is live." -ForegroundColor White
    Write-Host ""
    Write-Host "The first-run admin credentials line was not found in the Lahijan log."
    Write-Host "Expected when the DB was not empty on this boot."
    Write-Host ""
    Write-Host "Dashboard: $publicUrl"
}

Write-Ok "install.ps1 complete."
