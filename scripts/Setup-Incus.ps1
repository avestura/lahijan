# scripts/Setup-Incus.ps1 — bring up a REAL Incus daemon on Windows for Lahijan dev.
#
# Imports a fresh Ubuntu 24.04 WSL2 distro (non-admin; no Store), installs
# Incus 6.0 LTS via Zabbly, configures HTTPS+mTLS, ALSO installs the Incus
# CLI on Windows via winget (LinuxContainers.Incus) and wires it to the
# WSL2 daemon so `incus list` works from PowerShell/cmd directly.
# Idempotent — safe to re-run.
#
# === WHEN TO USE THIS SCRIPT (FALLBACK) ====================================
# Per ADR-0040 the canonical Lahijan dev topology is the containerized Incus
# in docker-compose.dev.yml (image ghcr.io/cmspam/incus-docker:lts, privileged,
# network_mode: host). Try that FIRST:
#
#   docker compose -f deployments/docker-compose.dev.yml up -d incus
#   docker compose -f deployments/docker-compose.dev.yml logs incus | Select-String vsock
#
# If the incus service fails with `socket: function not implemented`
# (AF_VSOCK unavailable inside Docker Desktop's VM), fall back to this
# WSL2-based setup. WSL2 distros ship a kernel with AF_VSOCK working.
# ===========================================================================
#
# Usage:
#   .\scripts\Setup-Incus.ps1                              # defaults
#   .\scripts\Setup-Incus.ps1 -InstallRoot D:\WSL          # custom install drive
#   .\scripts\Setup-Incus.ps1 -DistroName Incus -NoCerts
#   .\scripts\Setup-Incus.ps1 -SkipWingetInstall           # skip Windows CLI
#
# Prereqs: Docker Desktop running, WSL 2.7+ (wsl --version), ~5 GB free.
# Optional but recommended if you have a localhost proxy (VPN/corporate):
#   %USERPROFILE%\.wslconfig with [wsl2] networkingMode=mirrored
#                       and [experimental] hostAddressLoopback=true
#   then `wsl --shutdown` once before running this script.

[CmdletBinding()]
param(
    [string]$DistroName = "Incus",
    [string]$InstallRoot = "E:\WSL",
    [string]$CertName = "lahijan-dev-driver",
    [switch]$NoCerts,
    [switch]$NoLaunch,
    [switch]$SkipWingetInstall
)

$ErrorActionPreference = "Stop"

function Write-Step($m) { Write-Host "==> $m" -ForegroundColor Cyan }
function Write-Ok($m)   { Write-Host "OK:  $m" -ForegroundColor Green }
function Write-Warn($m) { Write-Host "WARN $m" -ForegroundColor Yellow }
function Write-Err($m)  { Write-Host "ERR  $m" -ForegroundColor Red }

# ----------------------------------------------------------------------------
# 0. Prerequisites
# ----------------------------------------------------------------------------

Write-Step "Checking prerequisites"
foreach ($c in @("docker", "wsl")) {
    if (-not (Get-Command $c -ErrorAction SilentlyContinue)) {
        Write-Err "Required command not on PATH: $c"; exit 1
    }
}
$wslVer = (wsl --version 2>$null | Select-String "WSL version" | Select-Object -First 1)
Write-Ok "docker + wsl present ($($wslVer -replace '^\s+',''))"

$installDir = Join-Path $InstallRoot $DistroName
$certDir    = Join-Path $InstallRoot "incus-certs"
New-Item -ItemType Directory -Path $InstallRoot -Force | Out-Null

# ----------------------------------------------------------------------------
# 1. Import Ubuntu 24.04 as a WSL2 distro (non-admin, no Store)
# ----------------------------------------------------------------------------

$distros = (wsl --list --quiet 2>$null) -replace "`0","" | ForEach-Object { $_.Trim() } | Where-Object { $_ -ne "" }
$exists = $distros -contains $DistroName

if (-not $exists) {
    Write-Step "Importing Ubuntu 24.04 as distro '$DistroName' (non-admin)"
    docker pull ubuntu:24.04 | Out-Null
    $root = "lahijan-noble-rootfs"
    docker rm -f $root 2>$null | Out-Null
    docker create --name $root ubuntu:24.04 true | Out-Null
    $tar = Join-Path $InstallRoot "noble-rootfs.tar"
    docker export $root -o $tar
    docker rm $root | Out-Null
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
    wsl --import $DistroName $installDir $tar --version 2 | Out-Null
    Remove-Item $tar -Force
    Write-Ok "Imported $DistroName -> $installDir"
} else {
    Write-Ok "Distro '$DistroName' already registered at $installDir"
}

# Helper: run bash in the distro as root, inherit nothing, fail-fast.
function Invoke-Incus([string]$script) {
    wsl -d $DistroName -u root -- bash -lc "set -e; $script" 2>&1
}

# ----------------------------------------------------------------------------
# 2. enable systemd + install the essentials the minimal image lacks
# ----------------------------------------------------------------------------

Write-Step "Ensuring systemd + base packages"
Invoke-Incus @'
# wsl.conf: enable systemd (Incus needs it for the incus.service unit).
if ! grep -q "^\\[boot\\]" /etc/wsl.conf 2>/dev/null; then
  printf "[boot]\nsystemd=true\n[user]\ndefault=root\n" > /etc/wsl.conf
  echo "wsl.conf written (needs wsl --terminate to take effect)"
fi
# Base packages the ubuntu:24.04 Docker image does NOT ship.
dpkg -l systemd kmod curl ca-certificates >/dev/null 2>&1 || {
  apt-get update -qq
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends systemd systemd-sysv kmod dbus curl ca-certificates
}
'@ | ForEach-Object { Write-Host "    $_" -ForegroundColor DarkGray }

# If systemd wasn't PID 1 before this run, restart the distro once.
$pid1 = (wsl -d $DistroName -u root -- bash -lc "ps -p 1 -o comm=" 2>$null).Trim()
if ($pid1 -ne "systemd") {
    Write-Warn "systemd not yet PID 1 (was '$pid1'); restarting $DistroName"
    wsl --terminate $DistroName 2>$null | Out-Null
    Start-Sleep -Seconds 2
    $pid1 = (wsl -d $DistroName -u root -- bash -lc "ps -p 1 -o comm=" 2>$null).Trim()
    if ($pid1 -ne "systemd") {
        Write-Err "systemd still not PID 1 (got '$pid1'). Check /etc/wsl.conf + 'wsl --shutdown'."
        exit 1
    }
}
Write-Ok "systemd is PID 1"

# ----------------------------------------------------------------------------
# 3. Install Incus 6.0 LTS via Zabbly (noble) + dnsmasq-base
# ----------------------------------------------------------------------------

Write-Step "Installing Incus via Zabbly (noble)"
Invoke-Incus @'
if ! command -v incus >/dev/null 2>&1; then
  mkdir -p /etc/apt/keyrings/
  curl -fsSL https://pkgs.zabbly.com/key.asc -o /etc/apt/keyrings/zabbly.asc
  echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/zabbly.asc] https://pkgs.zabbly.com/incus/stable noble main" \
    > /etc/apt/sources.list.d/zabbly.list
  apt-get update -qq
  # dnsmasq-base is NOT pulled by --no-install-recommends but incus admin init
  # needs it to build the incusbr0 bridge.
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends incus dnsmasq-base
fi
echo "incus $(incus --version)"
'@ | ForEach-Object { Write-Host "    $_" -ForegroundColor DarkGray }
Write-Ok "Incus installed"

# ----------------------------------------------------------------------------
# 4. Initialize the daemon + ensure the default profile has a root disk
# ----------------------------------------------------------------------------

Write-Step "Initializing Incus (storage pool + bridge + profile)"
Invoke-Incus @'
# init is mostly idempotent; --auto uses defaults (dir pool, incusbr0).
incus admin init --auto >/dev/null 2>&1 || true
# The preseed occasionally skips the root disk if init failed midway on a
# missing dep. Ensure it exists or every launch 404s with "No root device".
if ! incus profile show default | grep -q "type: disk"; then
  incus profile device add default root disk path=/ pool=default
fi
echo "profile devices:"; incus profile device list default
'@ | ForEach-Object { Write-Host "    $_" -ForegroundColor DarkGray }
Write-Ok "Incus initialized"

if ($NoLaunch) { Write-Step "-NoLaunch: skipping instance + cert setup"; exit 0 }

# ----------------------------------------------------------------------------
# 5. Expose HTTPS + generate/trust a client cert (mTLS)
# ----------------------------------------------------------------------------

if (-not $NoCerts) {
    Write-Step "Configuring HTTPS (:8443) + mTLS client cert"
    Invoke-Incus @"
incus config set core.https_address :8443
command -v openssl >/dev/null 2>&1 || apt-get install -y -qq openssl >/dev/null 2>&1
cd /root
[ -f lahijan-client.key ] || openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout lahijan-client.key -out lahijan-client.crt -days 3650 \
  -subj "/CN=$CertName" >/dev/null 2>&1
# Trust (idempotent): remove an existing entry with the same name first.
incus config trust list -f json 2>/dev/null | grep -q "$CertName" \
  || incus config trust add-certificate lahijan-client.crt --type=client --name $CertName
echo "trusted certs:"; incus config trust list | tail -n +2
"@ | ForEach-Object { Write-Host "    $_" -ForegroundColor DarkGray }

    # Copy the client cert to Windows so Lahijan (on the host) can use it.
    New-Item -ItemType Directory -Path $certDir -Force | Out-Null
    wsl -d $DistroName -u root -- bash -lc "cp /root/lahijan-client.crt /root/lahijan-client.key /mnt/$($InstallRoot.Substring(0,1).ToLower())/$(($certDir -replace '^.{2:3}\\','' -replace '\\','/').Substring(($InstallRoot.Length+1)))/ 2>/dev/null || cp /root/lahijan-client.* /mnt/" 2>&1 | Out-Null
    # The /mnt path translation above is fragile across drives; fall back to wsl read.
    $winCertPath = Join-Path $certDir "lahijan-client.crt"
    $winKeyPath  = Join-Path $certDir "lahijan-client.key"
    if (-not (Test-Path $winCertPath)) {
        wsl -d $DistroName -u root -- cat /root/lahijan-client.crt | Set-Content -Path $winCertPath -Encoding ASCII
        wsl -d $DistroName -u root -- cat /root/lahijan-client.key | Set-Content -Path $winKeyPath -Encoding ASCII
    }
    Write-Ok "Client cert copied to $certDir"
}

# ----------------------------------------------------------------------------
# 6. Smoke: launch a test container (best-effort; needs image pull)
# ----------------------------------------------------------------------------

Write-Step "Smoke test: launching images:ubuntu/24.04 as 'test1' (may pull ~150 MB)"
$smoke = Invoke-Incus @"
incus list -f json 2>/dev/null | grep -q '"test1"' || incus launch images:ubuntu/24.04 test1 2>&1 || echo 'launch skipped (network/profile)'
incus list 2>&1 | tail -n +2 | head -3
"@
$smoke | ForEach-Object { Write-Host "    $_" -ForegroundColor DarkGray }

# ----------------------------------------------------------------------------
# 6a. Install the Incus CLI on Windows (via winget) + wire it to the daemon
# ----------------------------------------------------------------------------
#
# The Incus DAEMON needs Linux (the WSL2 distro from step 1). The Incus CLIENT
# is a native Windows binary distributed via winget as LinuxContainers.Incus.
# Installing it lets the operator run `incus list` / `incus launch` / etc.
# from PowerShell or cmd directly — no `wsl -d Incus -u root --` prefix.
#
# Wiring steps:
#   a. winget install LinuxContainers.Incus (idempotent)
#   b. Copy the daemon-trusted client cert into %APPDATA%\incus\client.{crt,key}
#      (the Windows Incus CLI's global client cert location).
#   c. Pull the daemon's server.crt into %APPDATA%\incus\servercerts\wsl-incus.crt
#      so the CLI trusts the fingerprint without an interactive prompt.
#   d. Rewrite %APPDATA%\incus\config.yml to add a `wsl-incus` remote
#      pointing at https://localhost:8443 and set it as default.

if (-not $SkipWingetInstall) {
    Write-Step "Installing Incus CLI on Windows via winget"
    $winget = Get-Command winget -ErrorAction SilentlyContinue
    if (-not $winget) {
        Write-Warn "winget not available; skipping Windows CLI install."
        Write-Warn "Install Incus manually from https://linuxcontainers.org/incus/downloads/"
    } else {
        # `winget list` returns non-zero when the package is absent; treat both
        # as "not installed" to avoid false negatives.
        $installed = winget list --id LinuxContainers.Incus -e --source winget 2>$null | Select-String "LinuxContainers.Incus"
        if ($installed) {
            Write-Ok "Incus CLI already installed via winget"
        } else {
            winget install LinuxContainers.Incus --accept-source-agreements --accept-package-agreements 2>&1 |
                ForEach-Object { Write-Host "    $_" -ForegroundColor DarkGray }
            # Refresh PATH for this session so subsequent `incus` calls work.
            $env:Path = [System.Environment]::GetEnvironmentVariable("Path","Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path","User")
            $incusExe = Get-Command incus -ErrorAction SilentlyContinue
            if ($incusExe) {
                Write-Ok "Incus CLI installed: $($incusExe.Source)"
            } else {
                Write-Warn "Incus CLI installed but not on PATH yet. Open a new shell to use `incus`."
            }
        }
    }
}

if (-not $NoCerts -and (Get-Command incus -ErrorAction SilentlyContinue)) {
    Write-Step "Wiring Windows Incus CLI to the '$DistroName' daemon"

    # On Windows the Incus CLI reads from %APPDATA%\incus\ (NOT ~/.config/incus
    # like on Linux/macOS). Verified against Incus 7.2 + winget install.
    $winConfigDir = Join-Path $env:APPDATA "incus"
    $winServerCertDir = Join-Path $winConfigDir "servercerts"
    New-Item -ItemType Directory -Path $winConfigDir -Force | Out-Null
    New-Item -ItemType Directory -Path $winServerCertDir -Force | Out-Null

    # (a) Global client cert — the daemon already trusts this fingerprint
    #     (added in step 5 via `incus config trust add-certificate`).
    Copy-Item (Join-Path $certDir "lahijan-client.crt") (Join-Path $winConfigDir "client.crt") -Force
    Copy-Item (Join-Path $certDir "lahijan-client.key") (Join-Path $winConfigDir "client.key") -Force
    Write-Ok "Client cert installed at $winConfigDir\client.crt"

    # (b) Pull the daemon's server cert into servercerts\wsl-incus.crt so the
    #     CLI trusts the fingerprint without prompting.
    wsl -d $DistroName -u root -- bash -lc "cat /var/lib/incus/server.crt" |
        Set-Content -Path (Join-Path $winServerCertDir "wsl-incus.crt") -Encoding ASCII -Force
    Write-Ok "Server cert pulled to $winServerCertDir\wsl-incus.crt"

    # (c) Rewrite config.yml: keep defaults, add wsl-incus, set it as default.
    $configYml = @"
default-remote: wsl-incus
remotes:
  images:
    addr: https://images.linuxcontainers.org
    protocol: simplestreams
    public: true
  local:
    addr: unix://
    protocol: incus
    public: false
  wsl-incus:
    addr: https://localhost:8443
    auth_type: tls
    protocol: incus
    public: false
aliases: {}
defaults:
  list_format: ""
  console_type: ""
  console_spice_command: ""
"@
    Set-Content -Path (Join-Path $winConfigDir "config.yml") -Value $configYml -Encoding ASCII -Force
    Write-Ok "Remote 'wsl-incus' added as the default at $winConfigDir\config.yml"

    # (d) Verify.
    $verify = & incus list 2>&1
    if ($LASTEXITCODE -eq 0) {
        Write-Ok "Windows Incus CLI connected to the '$DistroName' daemon:"
        $verify | ForEach-Object { Write-Host "    $_" -ForegroundColor DarkGray }
    } else {
        Write-Warn "`incus list` failed (exit $LASTEXITCODE). Output:"
        $verify | ForEach-Object { Write-Host "    $_" -ForegroundColor Yellow }
        Write-Warn "If localhost:8443 is refused, add to .wslconfig:"
        Write-Warn "  [experimental]"
        Write-Warn "  hostAddressLoopback=true"
        Write-Warn "then: wsl --shutdown"
    }
}

# ----------------------------------------------------------------------------
# 7. Summary
# ----------------------------------------------------------------------------

Write-Host ""
Write-Host "================================================================" -ForegroundColor Green
Write-Host "  Incus is up in the '$DistroName' WSL2 distro" -ForegroundColor Green
Write-Host "================================================================" -ForegroundColor Green
Write-Host ""
if (-not $SkipWingetInstall -and (Get-Command incus -ErrorAction SilentlyContinue)) {
    Write-Host "  Manage it FROM WINDOWS (Incus CLI is installed + wired):" -ForegroundColor Cyan
    Write-Host "    incus list"
    Write-Host "    incus launch images:ubuntu/24.04 <name>"
    Write-Host "    incus exec <name> -- bash"
    Write-Host "    incus remote list     # default = wsl-incus"
    Write-Host ""
    Write-Host "  Or from inside the WSL2 distro:" -ForegroundColor DarkGray
    Write-Host "    wsl -d $DistroName -u root -- incus list" -ForegroundColor DarkGray
} else {
    Write-Host "  Manage it:" -ForegroundColor Cyan
    Write-Host "    wsl -d $DistroName -u root -- incus list"
    Write-Host "    wsl -d $DistroName -u root -- incus launch images:ubuntu/24.04 <name>"
    Write-Host "    wsl -d $DistroName -u root -- incus exec <name> -- bash"
}
Write-Host ""
if (-not $NoCerts) {
    Write-Host "  Wire Lahijan (add these to scripts\Run-Dev.ps1 or pass to the binary):" -ForegroundColor Cyan
    Write-Host "    --providers.incus.enabled"
    Write-Host "    --providers.incus.socketPath="
    Write-Host "    --providers.incus.remoteURL=https://localhost:8443"
    Write-Host "    --providers.incus.tls.clientCert=$(Join-Path $certDir 'lahijan-client.crt')"
    Write-Host "    --providers.incus.tls.clientKey=$(Join-Path $certDir 'lahijan-client.key')"
    Write-Host "    --providers.incus.tls.insecureSkipVerify"
    Write-Host ""
    Write-Host "  If localhost:8443 is refused from Windows, add to .wslconfig:" -ForegroundColor Yellow
    Write-Host "    [experimental]"
    Write-Host "    hostAddressLoopback=true"
    Write-Host "  then: wsl --shutdown"
}
Write-Host ""
Write-Host "  Docs: docs\howto\run-incus-on-windows.md" -ForegroundColor DarkGray
Write-Host "  Cleanup: wsl --unregister $DistroName" -ForegroundColor DarkGray
Write-Host ""
