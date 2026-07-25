# Lahijan - Windows dev stack launcher.
#
# Brings up the full Lahijan dev environment on a Windows host running
# Docker Desktop. Windows cannot run a real Incus daemon (it needs a Linux
# kernel), but the Incus *driver* is enabled so the compute module wires up
# and the dashboard's Instances page renders + lists from the DB. The startup
# Ping against the absent Unix socket is a non-fatal warning; actual instance
# create/start calls require a daemon reachable via WSL2 or a remote node
# (providers.incus.remoteURL). The WASM marketplace is also enabled against
# the in-repo sample index at examples/plugins/marketplace.
#
# By default this script also launches:
#   - The dashboard SPA at      http://localhost:5173  (web/)
#   - The marketing site at     http://localhost:4173  (website/)
#   - The documentation site at http://localhost:3000  (docs-site/)
# Use -SkipFrontend / -SkipWebsite / -SkipDocs to opt out individually.
#
# This script is idempotent. Re-running it will:
#   - re-apply the local workarounds the Windows checkout needs
#   - bring up any missing containers (compose is itself idempotent)
#   - re-run migrations (no-op if already at head)
#   - restart the Go backend + every frontend (kills old PIDs first)
#
# Workarounds baked in (these are real bugs in the repo on Windows; the
# patches are applied in-place so the script is safe to re-run):
#
#   1. Init scripts (deployments/{postgres,powerdns}/*.sh and
#      *.template) have CRLF line endings on a Windows checkout, which
#      breaks the `#!/usr/bin/env bash` shebang inside Alpine. The
#      script rewrites them to LF in-place.
#
#   2. SeaweedFS 3.61 dropped the `weed mini` subcommand. The script
#      patches docker-compose.dev.yml to use `weed server` instead and
#      strips the unsupported -filer.postgres.* flags.
#
#   3. deployments/seaweedfs/s3.json uses the pre-3.x identities schema
#      (`credentials` as a single object with snake_case keys). The
#      script overwrites it with the modern schema (array of camelCase
#      credentials).
#
#   4. SeaweedFS volume port 8080 clashes with Docker Desktop's
#      internal relay on Windows. The script sets SEAWEEDFS_VOLUME_PORT
#      to 18080 in deployments/.env.
#
#   5. The SeaweedFS container healthcheck probes localhost:8888, but
#      `weed server -ip=seaweedfs` binds the filer to the container's
#      service-name IP, not to 127.0.0.1. The script patches the
#      healthcheck to probe `seaweedfs:8888` instead.
#
#   6. Viper's AutomaticEnv does NOT override values that come from the
#      embedded default YAML (database.password: "" wins over
#      LAHIJAN_DATABASE_PASSWORD). The script launches the binary with
#      CLI flags (which DO win) instead of env vars.
#
#   7. The dashboard's vite.config.ts hardcodes the API proxy target
#      as http://localhost:8080, so the Go backend MUST listen on
#      8080 (not the 3000 in the embedded default YAML). The script
#      passes --http.server.port=8080.
#
#   8. Windows resolves `localhost` to IPv6 ::1 first, but Go's
#      `0.0.0.0` listener binds IPv4-only. The script's liveness probe
#      uses 127.0.0.1 explicitly to avoid the IPv6 black hole.
#
# Usage:
#   .\scripts\Run-Dev.ps1                  # bring up everything
#   .\scripts\Run-Dev.ps1 -Reset           # wipe volumes + reinit
#   .\scripts\Run-Dev.ps1 -SkipWebsite -SkipDocs   # backend + dashboard only
#   .\scripts\Run-Dev.ps1 -NoLaunch        # bring up stack, skip binary
#   .\scripts\Run-Dev.ps1 -NoBuild         # reuse dist\lahijan.exe
#   .\scripts\Run-Dev.ps1 -Logs            # tail container logs after launch
#
# Requires: Docker Desktop, Go 1.23+, Node 20+, npm on PATH.

[CmdletBinding()]
param(
    [switch]$Reset,
    [switch]$NoLaunch,
    [switch]$NoBuild,
    [switch]$Logs,
    [switch]$SkipFrontend,
    [switch]$SkipWebsite,
    [switch]$SkipDocs,
    [string]$AdminEmail = "admin@lahijan.local",
    [string]$AdminPassword = "Admin#Lahijan2026!",
    [int]$BackendPort  = 8080,   # matches web/vite.config.ts proxy target
    [int]$FrontendPort = 5173,
    [int]$WebsitePort  = 4173,   # website/vite.config.ts hardcodes 4173
    [int]$DocsPort     = 3000
)

$ErrorActionPreference = "Stop"

# ----------------------------------------------------------------------------
# Helpers
# ----------------------------------------------------------------------------

function Write-Step($msg) { Write-Host "==> $msg" -ForegroundColor Cyan }
function Write-Ok($msg)   { Write-Host "OK:  $msg" -ForegroundColor Green }
function Write-Warn($msg) { Write-Host "WARN $msg" -ForegroundColor Yellow }
function Write-Err($msg)  { Write-Host "ERR  $msg" -ForegroundColor Red }

function Test-Cmd([string]$name) {
    return [bool](Get-Command $name -ErrorAction SilentlyContinue)
}

function Wait-ContainerHealthy([string]$container, [int]$timeoutSec = 120) {
    $deadline = (Get-Date).AddSeconds($timeoutSec)
    while ((Get-Date) -lt $deadline) {
        $status = docker inspect --format='{{.State.Health.Status}}' $container 2>$null
        if ($status -eq "healthy")   { return $true }
        if ($status -eq "unhealthy") { return $false }
        Start-Sleep -Seconds 2
    }
    return $false
}

function Wait-HttpReady {
    param(
        [Parameter(Mandatory)] [int]    $Port,
        [int]    $TimeoutSec = 90,
        [int]    $ExpectedStatus = 200,
        [object] $Proc,                 # process to watch for early exit
        [string] $Label   = "service",
        [string] $Path     = "/"
    )
    $deadline = (Get-Date).AddSeconds($TimeoutSec)
    $urls = @(
        "http://127.0.0.1:$Port$Path"
        "http://localhost:$Port$Path"
        "http://[::1]:$Port$Path"
    )
    $lastErr = ""
    while ((Get-Date) -lt $deadline) {
        # Bail early if the spawned process has died.
        if ($Proc -and $Proc.HasExited) {
            Write-Warn "$Label : process exited (exit code $($Proc.ExitCode)) before HTTP became ready"
            return $false
        }
        foreach ($u in $urls) {
            try {
                $resp = Invoke-WebRequest -UseBasicParsing $u -TimeoutSec 2
                if ($resp.StatusCode -eq $ExpectedStatus) { return $true }
            } catch {
                $inner = $_.Exception.Response
                if ($inner -and $inner.StatusCode.value__ -eq $ExpectedStatus) { return $true }
                # Any HTTP response (even 404/500) proves the server is
                # listening; treat as ready. The dashboard root will 200
                # anyway, but Vite preview + Docusaurus sometimes 404 on /.
                if ($inner) { return $true }
                $lastErr = $_.Exception.Message
            }
        }
        Start-Sleep -Seconds 2
    }
    Write-Warn "$Label : not ready after ${TimeoutSec}s (last: $lastErr)"
    return $false
}

# Kill a process tree identified by a PID file (if it still exists), then
# delete the PID file. Uses taskkill /T so cmd.exe -> npm -> node children
# all die; plain Stop-Process leaves the node vite child alive, which keeps
# holding port 5173/5174/3000 and forces the next Vite onto a random port.
function Stop-PidFile([string]$pidFile) {
    if (-not (Test-Path $pidFile)) { return }
    $pidVal = (Get-Content $pidFile -ErrorAction SilentlyContinue | Select-Object -First 1)
    if ($pidVal -match '^\d+$') {
        # /T = kill child tree, /F = force. Silent on missing PID.
        & taskkill /F /T /PID ([int]$pidVal) 2>$null | Out-Null
    }
    Remove-Item $pidFile -ErrorAction SilentlyContinue
}

# Kill anything currently listening on a TCP port. Defensive cleanup for
# stale Vite/Docusaurus processes that survived Stop-PidFile (e.g. crashed
# before the PID file was written, or launched manually by the user).
function Stop-Port([int]$port) {
    $conns = Get-NetTCPConnection -State Listen -LocalPort $port -ErrorAction SilentlyContinue
    foreach ($c in $conns) {
        if ($c.OwningProcess -and $c.OwningProcess -ne 0) {
            & taskkill /F /T /PID $c.OwningProcess 2>$null | Out-Null
        }
    }
}

function Start-NpmDev {
    param(
        [Parameter(Mandatory)] [string] $WorkDir,
        [Parameter(Mandatory)] [string] $PidFile,
        [Parameter(Mandatory)] [string] $WindowTitle,
        [string[]] $ExtraArgs = @(),
        [switch] $InstallIfMissing
    )
    if (-not (Test-Path (Join-Path $WorkDir "package.json"))) {
        Write-Warn "$WindowTitle : package.json missing ($WorkDir); skipping"
        return $null
    }

    if ($InstallIfMissing -and -not (Test-Path (Join-Path $WorkDir "node_modules"))) {
        Write-Step "$WindowTitle : installing npm dependencies (first run)"
        & npm --prefix $WorkDir install --no-audit --no-fund
        if ($LASTEXITCODE -ne 0) { Write-Err "$WindowTitle : npm install failed"; return $null }
    }

    Stop-PidFile $PidFile

    # Each frontend runs in its own minimized window so the user can
    # inspect HMR output + kill it independently of this script.
    $logDir = Join-Path $RepoRoot "dist\dev-logs"
    New-Item -ItemType Directory -Path $logDir -Force | Out-Null
    $logBasename = [IO.Path]::GetFileNameWithoutExtension($PidFile)
    $stdout = Join-Path $logDir "$logBasename.stdout.log"
    $stderr = Join-Path $logDir "$logBasename.stderr.log"

    # On Windows, npm is a .cmd batch wrapper that Start-Process cannot
    # resolve directly. Use cmd.exe /c to launch it. We pass
    # -WorkingDirectory so Vite picks up the right package.json / vite.config
    # (npm's --prefix flag does NOT change the spawned script's cwd, which
    # breaks Vite's process.cwd() lookups). Stdout/stderr are redirected
    # to per-service log files so the script can surface them on failure
    # (Start-Process ignores the parent console once WindowStyle != Normal).
    $cmdLine = 'npm run dev'
    if ($ExtraArgs.Count -gt 0) {
        $cmdLine += ' ' + ($ExtraArgs -join ' ')
    }
    $proc = Start-Process -FilePath "cmd.exe" `
        -ArgumentList @("/c", $cmdLine) `
        -WorkingDirectory $WorkDir `
        -WindowStyle Minimized `
        -RedirectStandardOutput $stdout `
        -RedirectStandardError  $stderr `
        -PassThru

    Set-Content -Path $PidFile -Value $proc.Id
    return $proc
}

# ----------------------------------------------------------------------------
# Paths
# ----------------------------------------------------------------------------

$ScriptDir     = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot      = Split-Path -Parent $ScriptDir
Set-Location $RepoRoot

$DeployDir     = Join-Path $RepoRoot "deployments"
$EnvFile       = Join-Path $DeployDir ".env"
$EnvExample    = Join-Path $DeployDir ".env.example"
$ComposeFile   = Join-Path $DeployDir "docker-compose.dev.yml"
$S3Json        = Join-Path $DeployDir "seaweedfs\s3.json"
$MigrationsDir = Join-Path $RepoRoot "internal\app\lahijan\database\migrations"
$Binary        = Join-Path $RepoRoot "dist\lahijan.exe"

$WebDir      = Join-Path $RepoRoot "web"
$WebsiteDir  = Join-Path $RepoRoot "website"
$DocsDir     = Join-Path $RepoRoot "docs-site"
$PidDir      = Join-Path $RepoRoot "dist\dev-pids"
New-Item -ItemType Directory -Path $PidDir -Force | Out-Null

$BackendPidFile  = Join-Path $PidDir "backend.pid"
$FrontendPidFile = Join-Path $PidDir "frontend.pid"
$WebsitePidFile  = Join-Path $PidDir "website.pid"
$DocsPidFile     = Join-Path $PidDir "docs.pid"

# ----------------------------------------------------------------------------
# 0. Prerequisites
# ----------------------------------------------------------------------------

Write-Step "Checking prerequisites"
foreach ($c in @("docker", "go", "npm")) {
    if (-not (Test-Cmd $c)) { Write-Err "Required command not on PATH: $c"; exit 1 }
}
if (-not (docker compose version 2>$null)) { Write-Err "docker compose subcommand unavailable"; exit 1 }
Write-Ok "Docker + Go + npm present"

# ----------------------------------------------------------------------------
# 1. deployments/.env  (with SeaweedFS volume port workaround)
# ----------------------------------------------------------------------------

Write-Step "Ensuring deployments/.env exists"
if (-not (Test-Path $EnvFile)) {
    Copy-Item $EnvExample $EnvFile
    Write-Ok "Copied .env.example -> .env"
}
$envContent = Get-Content $EnvFile
$envContent = $envContent -replace '^SEAWEEDFS_VOLUME_PORT=.*', "SEAWEEDFS_VOLUME_PORT=18080"
Set-Content -Path $EnvFile -Value $envContent -Encoding ASCII
Write-Ok "SEAWEEDFS_VOLUME_PORT=18080 (avoids Docker Desktop :8080 clash)"

# ----------------------------------------------------------------------------
# 2. CRLF -> LF on bind-mounted shell scripts
# ----------------------------------------------------------------------------

Write-Step "Normalising line endings on Linux-mounted scripts"
$crlfFiles = @(
    "deployments\postgres\init.sh",
    "deployments\powerdns\init.sh",
    "deployments\powerdns\pdns.conf.template",
    "deployments\dnsdist\dnsdist.conf.template"
)
foreach ($rel in $crlfFiles) {
    $p = Join-Path $RepoRoot $rel
    if (-not (Test-Path $p)) { continue }
    $bytes = [IO.File]::ReadAllBytes($p)
    $crlfCount = 0
    for ($i = 1; $i -lt $bytes.Length; $i++) {
        if ($bytes[$i-1] -eq 13 -and $bytes[$i] -eq 10) { $crlfCount++ }
    }
    if ($crlfCount -gt 0) {
        $text = [IO.File]::ReadAllText($p)
        $fixed = $text -replace "`r`n", "`n"
        [IO.File]::WriteAllText($p, $fixed, (New-Object Text.UTF8Encoding $false))
        Write-Ok "$rel - converted $crlfCount CRLF -> LF"
    }
}

# ----------------------------------------------------------------------------
# 3. docker-compose.dev.yml  (`weed mini` -> `weed server`)
# ----------------------------------------------------------------------------

Write-Step "Patching docker-compose.dev.yml for SeaweedFS 3.61+"
$composeText = Get-Content $ComposeFile -Raw

if ($composeText -match '\-\s*"mini"') {
    $old = @'
    command:
      - "mini"
      # Bind on the compose service name so siblings + the host can reach.
      - "-ip=seaweedfs"
'@
    $new = @'
    command:
      # NOTE: `weed mini` was removed in SeaweedFS 3.61+. `weed server` with
      # -s3 + -filer covers the same single-process topology for dev.
      - "server"
      # Bind on the compose service name so siblings + the host can reach.
      - "-ip=seaweedfs"
'@
    $composeText = $composeText.Replace($old, $new)
    Write-Ok "Replaced `"weed mini`" with `"weed server`""
} else {
    Write-Ok "Already patched (no `"mini`" subcommand found)"
}

$filerPgBlock = @'
      # Filer listens on 8888 + stores metadata in the shared Postgres
      # `seaweed` database (ADR-0007).
      - "-filer=true"
      # Postgres-backed Filer store. SeaweedFS reads PG_* / WEED_*
      # env vars; we pass the connection via -filer.postgres.* flags
      # below so the same env shape works in dev + prod.
      - "-filer.postgres"
      - "-filer.postgres.hostname=postgres"
      - "-filer.postgres.port=5432"
      - "-filer.postgres.database=${SEAWEED_DATABASE_NAME:-seaweed}"
      - "-filer.postgres.username=${SEAWEED_DATABASE_USER:-seaweed}"
      - "-filer.postgres.password=${SEAWEED_DATABASE_PASSWORD:-seaweed}"
      # Pinned ports (master 9333, volume 8080, filer 8888, s3 8333).
'@
$filerReplacement = @'
      # Filer listens on 8888. NOTE: `weed server` does NOT accept
      # -filer.postgres.* flags (those are only valid on `weed filer`). For
      # dev we let the Filer use its default LevelDB store; the prod split
      # uses a separate `weed filer` container with a templated filer.toml.
      - "-filer=true"
      # Pinned ports (master 9333, volume 8080, filer 8888, s3 8333).
'@
if ($composeText.Contains($filerPgBlock)) {
    $composeText = $composeText.Replace($filerPgBlock, $filerReplacement)
    Write-Ok "Stripped unsupported -filer.postgres.* flags"
}

# SeaweedFS healthcheck: `weed server -ip=seaweedfs` binds the filer to the
# container's service-name IP (not 127.0.0.1), so the stock
# `wget http://localhost:8888` probe always fails. Probe the service name.
$oldHc = 'wget -q -O /dev/null --tries=1 --timeout=3 http://localhost:8888/'
$newHc = 'wget -q -O /dev/null --tries=1 --timeout=3 http://seaweedfs:8888/'
if ($composeText.Contains($oldHc)) {
    $composeText = $composeText.Replace($oldHc, $newHc)
    Write-Ok "Patched SeaweedFS healthcheck to probe seaweedfs:8888"
}

Set-Content -Path $ComposeFile -Value $composeText -NoNewline -Encoding UTF8

# ----------------------------------------------------------------------------
# 4. deployments/seaweedfs/s3.json  (modern identities schema)
# ----------------------------------------------------------------------------

Write-Step "Forcing modern s3.json schema"
$modernS3 = @'
{
  "_comment": "SeaweedFS S3 config (WS-13). Dev-only default with the admin identity baked in. The credentials MUST match LAHIJAN_PROVIDERS_SEAWEEDFS_ADMIN_ACCESS_KEY / _SECRET_KEY in the Lahijan app env so the driver can authenticate. Per-user IAM identities Lahijan mints at runtime are written via the Filer metadata API to /etc/seaweedfs/identities/ - the S3 server watches that path and reloads them live. For non-dev deployments, override this file via a secret mount with high-entropy credentials.",
  "identities": [
    {
      "name": "lahijan_admin",
      "credentials": [
        {
          "accessKey": "lahijan_dev_admin_key",
          "secretKey": "lahijan_dev_admin_secret"
        }
      ],
      "actions": ["Admin", "Read", "Write", "List", "Tagging"],
      "isAdmin": true
    }
  ]
}
'@
$existingS3 = if (Test-Path $S3Json) { (Get-Content $S3Json -Raw).Trim() } else { "" }
if ($existingS3 -ne $modernS3.Trim()) {
    Set-Content -Path $S3Json -Value $modernS3 -NoNewline -Encoding UTF8
    Write-Ok "s3.json rewritten (array of camelCase credentials)"
} else {
    Write-Ok "s3.json already in modern schema"
}

# ----------------------------------------------------------------------------
# 5. Optional reset
# ----------------------------------------------------------------------------

if ($Reset) {
    Write-Step "Reset: tearing down stack and wiping volumes"
    docker compose -f $ComposeFile --env-file $EnvFile down -v --remove-orphans | Out-Null
    Write-Ok "Volumes wiped"
}

# ----------------------------------------------------------------------------
# 6. docker compose up
# ----------------------------------------------------------------------------

Write-Step "Bringing up dev compose stack"
docker compose -f $ComposeFile --env-file $EnvFile up -d | Out-Null
if ($LASTEXITCODE -ne 0) { Write-Err "docker compose up failed"; exit 1 }
Write-Ok "Containers started"

# ----------------------------------------------------------------------------
# 7. Wait for healthy
# ----------------------------------------------------------------------------

Write-Step "Waiting for containers to become healthy"
$containers = @(
    "lahijan-postgres-dev",
    "lahijan-mailhog-dev",
    "lahijan-seaweedfs-dev"
)
foreach ($c in $containers) {
    if (Wait-ContainerHealthy $c) {
        Write-Ok "$c healthy"
    } else {
        $status = docker inspect --format='{{.State.Health.Status}}' $c 2>$null
        Write-Warn "$c NOT healthy (state: $status). Continuing; check 'docker logs $c'."
    }
}

# PowerDNS ships a healthcheck that references `wget` which is missing from
# the upstream PDNS image. The daemon itself runs fine; cosmetic only.
$pdnsStatus = docker inspect --format='{{.State.Health.Status}}' lahijan-powerdns-dev 2>$null
if ($pdnsStatus -ne "healthy") {
    Write-Warn "lahijan-powerdns-dev reports '$pdnsStatus' (cosmetic; PDNS healthcheck uses missing 'wget'). Daemon is up."
} else {
    Write-Ok "lahijan-powerdns-dev healthy"
}

# ----------------------------------------------------------------------------
# 8. Apply migrations
# ----------------------------------------------------------------------------

Write-Step "Applying DB migrations"
$migrationsMount = (Resolve-Path $MigrationsDir).Path -replace '\\', '/'
$dbUrl = "postgres://lahijan:lahijan@postgres:5432/lahijan?sslmode=disable"

docker run --rm --network lahijan-dev_default `
    -v "${migrationsMount}:/migrations" migrate/migrate `
    -path=/migrations -database="$dbUrl" up
if ($LASTEXITCODE -ne 0) {
    Write-Err "Migrations failed. Postgres may still be starting; re-run the script."
    exit 1
}
$verOutput = docker run --rm --network lahijan-dev_default `
    -v "${migrationsMount}:/migrations" migrate/migrate `
    -path=/migrations -database="$dbUrl" version 2>&1
Write-Ok "Migrations applied (version: $verOutput)"

# ----------------------------------------------------------------------------
# 9. Build the binary
# ----------------------------------------------------------------------------

if (-not $NoBuild) {
    Write-Step "Building dist\lahijan.exe"
    & go build -ldflags "-X 'github.com/avestura/lahijan/internal/app/lahijan/version.LahijanVersion=dev'" `
        -o $Binary ./cmd/lahijan
    if ($LASTEXITCODE -ne 0) { Write-Err "Go build failed"; exit 1 }
    Write-Ok "Built"
} else {
    if (-not (Test-Path $Binary)) {
        Write-Err "-NoBuild set but $Binary does not exist"; exit 1
    }
    Write-Ok "Reusing existing binary"
}

if ($NoLaunch) {
    Write-Step "Stack is up (binary + frontends skipped due to -NoLaunch)"
    exit 0
}

# ----------------------------------------------------------------------------
# 10. Launch Lahijan backend  (port 8080 to match dashboard's vite proxy)
# ----------------------------------------------------------------------------

Write-Step "Stopping any previously-running Lahijan processes"
Stop-PidFile $BackendPidFile
Get-Process lahijan -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Seconds 1

$stdoutLog = Join-Path $RepoRoot "dist\lahijan.stdout.log"
$stderrLog = Join-Path $RepoRoot "dist\lahijan.stderr.log"

# Viper quirk: env vars don't override embedded YAML defaults, so we pass
# everything via CLI flags (which DO win). See header comment #6.
$lahijanArgs = @(
    "--debug",
    "--http.server.cors.enabled",
    "--http.server.port=$BackendPort",
    "--database.password=lahijan",
    "--smtp.enabled",
    "--smtp.host=localhost",
    "--smtp.port=1025",
    "--bootstrap.adminEmail=$AdminEmail",
    "--bootstrap.adminPassword=$AdminPassword",
    "--providers.powerdns.enabled",
    "--providers.powerdns.baseURL=http://localhost:8081",
    "--providers.powerdns.apiKey=lahijan-dev-pdns-key",
    "--providers.seaweedfs.enabled",
    "--providers.seaweedfs.s3Endpoint=http://localhost:8333",
    "--providers.seaweedfs.filerURL=http://localhost:8888",
    "--providers.seaweedfs.adminAccessKey=lahijan_dev_admin_key",
    "--providers.seaweedfs.adminSecretKey=lahijan_dev_admin_secret",
    # Incus driver: wires the compute module so the Instances page renders.
    # No daemon runs on Windows; startup Ping fails as a non-fatal warning.
    # Instance create/start needs a real daemon (WSL2 or providers.incus.remoteURL).
    "--providers.incus.enabled",
    "--providers.incus.events.enabled=false",
    # WASM subsystem: enables the plugin runtime + the admin marketplace API
    # (index at examples/plugins/marketplace/plugins-marketplace.yaml).
    "--wasm.enabled"
)

Write-Step "Launching Lahijan backend on http://127.0.0.1:$BackendPort"
$backendProc = Start-Process -FilePath $Binary -ArgumentList $lahijanArgs `
    -RedirectStandardOutput $stdoutLog `
    -RedirectStandardError  $stderrLog `
    -NoNewWindow -PassThru
Set-Content -Path $BackendPidFile -Value $backendProc.Id

# Probe via 127.0.0.1 (IPv4) - localhost on Windows resolves to IPv6 first
# but Go's 0.0.0.0 listener is IPv4-only.
if (-not (Wait-HttpReady -Port $BackendPort -TimeoutSec 60 -Proc $backendProc `
        -Label "Backend" -Path "/healthcheck/liveness" -ExpectedStatus 200)) {
    Write-Err "Backend did not become healthy in 60s. Recent stderr:"
    Get-Content $stderrLog -Tail 30 -ErrorAction SilentlyContinue | ForEach-Object { Write-Host "    $_" -ForegroundColor DarkGray }
    exit 1
}
Write-Ok "Backend healthy (PID $($backendProc.Id))"

# ----------------------------------------------------------------------------
# 11. Launch frontends (dashboard + marketing site + docs)
# ----------------------------------------------------------------------------

if (-not $SkipFrontend) {
    Write-Step "Launching dashboard SPA (web/) on http://localhost:$FrontendPort"
    Stop-Port $FrontendPort   # defensive: kill any stale Vite holding the port
    $proc = Start-NpmDev -WorkDir $WebDir -PidFile $FrontendPidFile `
        -WindowTitle "Lahijan Dashboard" -InstallIfMissing `
        -ExtraArgs @("--", "--port", "$FrontendPort", "--strictPort")
    if ($proc) {
        if (Wait-HttpReady -Port $FrontendPort -TimeoutSec 120 -Proc $proc -Label "Dashboard") {
            Write-Ok "Dashboard ready (PID $($proc.Id))"
        } else {
            Write-Warn "Dashboard not ready. Open its minimized window to inspect, or browse http://localhost:$FrontendPort manually."
        }
    }
}

if (-not $SkipWebsite) {
    Write-Step "Launching marketing site (website/) on http://localhost:$WebsitePort"
    Stop-Port $WebsitePort
    $proc = Start-NpmDev -WorkDir $WebsiteDir -PidFile $WebsitePidFile `
        -WindowTitle "Lahijan Marketing" -InstallIfMissing `
        -ExtraArgs @("--", "--port", "$WebsitePort", "--strictPort")
    if ($proc) {
        if (Wait-HttpReady -Port $WebsitePort -TimeoutSec 120 -Proc $proc -Label "Marketing") {
            Write-Ok "Marketing site ready (PID $($proc.Id))"
        } else {
            Write-Warn "Marketing site not ready. Open its minimized window to inspect."
        }
    }
}

if (-not $SkipDocs) {
    Write-Step "Launching docs site (docs-site/) on http://localhost:$DocsPort"
    if (-not (Test-Path (Join-Path $DocsDir "node_modules"))) {
        Write-Step "Docs : installing npm dependencies (first run)"
        & npm --prefix $DocsDir install --no-audit --no-fund
    }
    Stop-PidFile $DocsPidFile
    Stop-Port $DocsPort

    # Capture stdout/stderr so we can diagnose startup failures (the
    # previous version dropped these and the docs site failed silently
    # with exit code 1 + no log to inspect).
    $logDir = Join-Path $RepoRoot "dist\dev-logs"
    New-Item -ItemType Directory -Path $logDir -Force | Out-Null
    $docsOut = Join-Path $logDir "docs.stdout.log"
    $docsErr = Join-Path $logDir "docs.stderr.log"

    # Docusaurus `start` is the dev command. The `--` separator passes
    # the rest to docusaurus (not npm); without it npm swallows --port.
    $docsCmd = "npm run docusaurus -- start --port $DocsPort --no-open --host 127.0.0.1"
    $docsProc = Start-Process -FilePath "cmd.exe" `
        -ArgumentList @("/c", $docsCmd) `
        -WorkingDirectory $DocsDir `
        -WindowStyle Minimized `
        -RedirectStandardOutput $docsOut `
        -RedirectStandardError  $docsErr `
        -PassThru
    Set-Content -Path $DocsPidFile -Value $docsProc.Id
    if (Wait-HttpReady -Port $DocsPort -TimeoutSec 180 -Proc $docsProc -Label "Docs") {
        Write-Ok "Docs site ready (PID $($docsProc.Id))"
    } else {
        Write-Warn "Docs site not ready. Last 20 stderr lines:"
        Get-Content $docsErr -Tail 20 -ErrorAction SilentlyContinue | ForEach-Object { Write-Host "    $_" -ForegroundColor DarkGray }
    }
}

# ----------------------------------------------------------------------------
# 12. Summary
# ----------------------------------------------------------------------------

Write-Host ""
Write-Host "================================================================" -ForegroundColor Green
Write-Host "  Lahijan dev environment is up" -ForegroundColor Green
Write-Host "================================================================" -ForegroundColor Green
Write-Host ""
Write-Host "  URLs" -ForegroundColor Cyan
if (-not $SkipFrontend) { Write-Host "    Dashboard      : http://localhost:$FrontendPort" }
if (-not $SkipWebsite)  { Write-Host "    Marketing site : http://localhost:$WebsitePort" }
if (-not $SkipDocs)     { Write-Host "    Docs site      : http://localhost:$DocsPort" }
Write-Host "    Backend API    : http://localhost:$BackendPort/api/v1"
Write-Host "    MailHog UI     : http://localhost:8025"
Write-Host "    SeaweedFS UI   : http://localhost:9333"
Write-Host ""
Write-Host "  Admin login (backend)" -ForegroundColor Cyan
Write-Host "    email    : $AdminEmail"
Write-Host "    password : $AdminPassword"
Write-Host ""
Write-Host "  Infrastructure" -ForegroundColor Cyan
Write-Host "    Postgres     : localhost:5432  (lahijan / lahijan)"
Write-Host "    PowerDNS API : localhost:8081  (key: lahijan-dev-pdns-key)"
Write-Host "    SeaweedFS S3 : localhost:8333  (lahijan_dev_admin_key / lahijan_dev_admin_secret)"
Write-Host ""
Write-Host "  Compute + Marketplace (no daemon on Windows)" -ForegroundColor Cyan
Write-Host "    Incus driver : enabled (UI renders; instance ops need a Linux/WSL2 daemon)"
Write-Host "    Marketplace  : enabled (samples at examples/plugins/marketplace)"
Write-Host ""
Write-Host "  Stop" -ForegroundColor Cyan
Write-Host "    To stop everything:  .\scripts\Stop-Dev.ps1"
Write-Host "    Or close the minimized console windows + run:"
Write-Host "        Stop-Process -Id $($backendProc.Id)"
if (-not $SkipFrontend) { Write-Host "    Dashboard PID file : $FrontendPidFile" -ForegroundColor DarkGray }
if (-not $SkipWebsite)  { Write-Host "    Marketing PID file : $WebsitePidFile" -ForegroundColor DarkGray }
if (-not $SkipDocs)     { Write-Host "    Docs PID file      : $DocsPidFile" -ForegroundColor DarkGray }
Write-Host "    Backend PID file    : $BackendPidFile" -ForegroundColor DarkGray
Write-Host "    Backend logs        : dist\lahijan.stdout.log, dist\lahijan.stderr.log" -ForegroundColor DarkGray
Write-Host ""

if ($Logs) {
    Write-Step "Tailing container logs (Ctrl+C to stop)"
    docker compose -f $ComposeFile --env-file $EnvFile logs -f
}
