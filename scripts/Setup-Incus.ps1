# scripts/Setup-Incus.ps1 — bring up a REAL Incus daemon on Windows for Lahijan dev.
#
# Imports a fresh Ubuntu 24.04 WSL2 distro (non-admin; no Store), installs
# Incus 6.0 LTS via Zabbly, configures HTTPS+mTLS, and prints the exact
# --providers.incus.* flags to pass Lahijan. Idempotent — safe to re-run.
#
# Why a WSL2 distro and not Docker: Incus fatality-binds a vsock listener at
# startup, and Docker Desktop's VM blocks AF_VSOCK (socket() -> ENOSYS), so
# every Docker-based Incus image dies with "listen vsock ...: function not
# implemented". AF_VSOCK works in a WSL2 distro. See
# docs/howto/run-incus-on-windows.md + .opencode/skills/incus-on-windows/SKILL.md.
#
# Usage:
#   .\scripts\Setup-Incus.ps1                          # defaults
#   .\scripts\Setup-Incus.ps1 -InstallRoot D:\WSL      # custom install drive
#   .\scripts\Setup-Incus.ps1 -DistroName Incus -NoCerts
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
    [switch]$NoLaunch
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
# 7. Summary
# ----------------------------------------------------------------------------

Write-Host ""
Write-Host "================================================================" -ForegroundColor Green
Write-Host "  Incus is up in the '$DistroName' WSL2 distro" -ForegroundColor Green
Write-Host "================================================================" -ForegroundColor Green
Write-Host ""
Write-Host "  Manage it:" -ForegroundColor Cyan
Write-Host "    wsl -d $DistroName -u root -- incus list"
Write-Host "    wsl -d $DistroName -u root -- incus launch images:ubuntu/24.04 <name>"
Write-Host "    wsl -d $DistroName -u root -- incus exec <name> -- bash"
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
