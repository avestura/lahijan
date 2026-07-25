# Run a real Incus on Windows for Lahijan dev

> **TL;DR** Run `.\scripts\Setup-Incus.ps1`. It imports an Ubuntu 24.04
> WSL2 distro, installs Incus 6.0 LTS, configures HTTPS+mTLS, and prints
> the exact flags to pass Lahijan. No admin/Store required.

This guide stands up a **real** Incus daemon on a Windows host so you can
exercise the compute module (WS-14) end-to-end. The in-process fake under
`providers/incus/fake/` is fine for unit tests, but it does not catch
wire-level mismatches between the driver and a real daemon — two real
driver bugs were found by following this guide (see *Troubleshooting*).

If you only want the "why", read `.opencode/skills/incus-on-windows/SKILL.md`.

## Why a WSL2 distro (and not Docker)

Incus's daemon fatality-binds a **vsock listener** at startup. `socket(AF_VSOCK)`
returns `ENOSYS` inside a Docker Desktop container (Docker Desktop's VM blocks
vsock), so **every Docker-based Incus image dies** with
`listen vsock host(2):…: function not implemented`. AF_VSOCK **does** work in a
WSL2 distro (kernel 6.x). So: use a WSL2 distro, not Docker.

## Prerequisites

- Windows 11 (or Windows 10 with WSL2) + **WSL 2.7+** (`wsl --version`).
- Docker Desktop running (only used to materialise the Ubuntu rootfs — Incus
  itself does not run in Docker).
- ~5 GB free on the target drive (the distro vhdx + Incus + one image).
- A `.wslconfig` with **mirrored networking** if your Windows host has a
  localhost proxy (VPN/corporate). Without it, image downloads stall at ~0 B/s:

  ```ini
  # %USERPROFILE%\.wslconfig
  [wsl2]
  networkingMode=mirrored
  [experimental]
  hostAddressLoopback=true   # so Windows can reach the daemon at localhost:8443
  sparseVhd=true
  ```
  Apply with `wsl --shutdown` (restarts all distros, including Docker — your
  dev containers will auto-restart).

## Option A — automated (recommended)

```powershell
.\scripts\Setup-Incus.ps1
# Custom install path / distro name:
.\scripts\Setup-Incus.ps1 -InstallRoot D:\WSL -DistroName Incus
```

The script is idempotent — safe to re-run. It will:

1. Import Ubuntu 24.04 into a WSL2 distro (from the `ubuntu:24.04` Docker image).
2. Enable systemd + install the essentials the minimal image lacks.
3. Install Incus 6.0 LTS via Zabbly + `dnsmasq-base`.
4. Run `incus admin init --auto` + add the default profile's root disk.
5. Expose the daemon on `:8443` (HTTPS) + generate/trust a client cert.
6. Copy the client cert to `<InstallRoot>\incus-certs\` on Windows.
7. Print the exact `--providers.incus.*` flags for `scripts/Run-Dev.ps1`.

## Option B — manual

See `.opencode/skills/incus-on-windows/SKILL.md` for the step-by-step commands.
The short version:

```powershell
# 1. Import Ubuntu 24.04 (non-admin)
docker pull ubuntu:24.04
docker create --name noble-rootfs ubuntu:24.04 true
docker export noble-rootfs -o E:\WSL\noble-rootfs.tar
docker rm noble-rootfs
wsl --import Incus E:\WSL\Incus E:\WSL\noble-rootfs.tar --version 2
```

```bash
# 2. Inside the distro (wsl -d Incus -u root)
printf '[boot]\nsystemd=true\n[user]\ndefault=root\n' > /etc/wsl.conf
apt update && apt install -y systemd systemd-sysv kmod dbus curl ca-certificates
# (then wsl --terminate Incus from Windows, and re-enter)
curl -fsSL https://pkgs.zabbly.com/key.asc -o /etc/apt/keyrings/zabbly.asc
echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/zabbly.asc] https://pkgs.zabbly.com/incus/stable noble main" > /etc/apt/sources.list.d/zabbly.list
apt update && apt install -y --no-install-recommends incus dnsmasq-base
incus admin init --auto
incus profile device add default root disk path=/ pool=default
incus launch images:ubuntu/24.04 test1
incus exec test1 -- echo alive
```

## Networking trade-off (important)

WSL2 on a host with a localhost proxy has a painful either/or:

| `.wslconfig` mode | Image pulls | Container bridge (`incusbr0`) |
|---|---|---|
| `mirrored` | ✅ fast | ❌ "Network unavailable" |
| `nat` (default) | ❌ choked | ✅ works |

For **compute dev** (create/start/exec — what the driver does), `mirrored`
is fine: containers run without an IP. If a launch fails with
`Network "incusbr0" unavailable`, drop `eth0` from the profile:
`incus profile device remove default eth0`. Full container networking
(instances with IPs) needs NAT mode **and** no localhost proxy.

## Pointing Lahijan at the daemon

After setup, pass these to `Run-Dev.ps1` (or the binary directly). Viper's
env vars don't override the embedded YAML defaults, so use **flags**:

```
--providers.incus.enabled
--providers.incus.socketPath=
--providers.incus.remoteURL=https://localhost:8443
--providers.incus.tls.clientCert=<contents of incus-certs\lahijan-client.crt>
--providers.incus.tls.clientKey=<contents of incus-certs\lahijan-client.key>
--providers.incus.tls.insecureSkipVerify
```

If `localhost:8443` is refused from Windows, ensure `[experimental]
hostAddressLoopback=true` is in `.wslconfig` and you've run `wsl --shutdown`.

## Verifying the driver without running the full backend

The fastest end-to-end check is a tiny cross-compiled binary that imports
`providers/incus` and calls `Ping` + `ListInstances` against `localhost:8443`
**inside the distro** (where networking always works). Pattern:

```go
// cmd/incus-smoke/main.go (temporary; delete after verifying)
package main
import ("context";"fmt";"log";"os";"time"
  "github.com/avestura/lahijan/internal/app/lahijan/providers/incus")
func main() {
  cert,_ := os.ReadFile(os.Getenv("CLIENT_CERT"))
  key,_  := os.ReadFile(os.Getenv("CLIENT_KEY"))
  p,err := incus.NewRemoteClient("https://localhost:8443", incus.TLSConfig{
    ClientCert:string(cert), ClientKey:string(key), InsecureSkipVerify:true,
  }, incus.Config{RequestTimeout:15*time.Second})
  if err!=nil { log.Fatal(err) }
  if err:=p.Ping(context.Background()); err!=nil { log.Fatal(err) }
  fmt.Println("PING OK")
  insts,_ := p.ListInstances(context.Background(),"default")
  fmt.Printf("%d instances\n", len(insts))
}
```

```powershell
$env:GOOS="linux"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"
go build -o E:\WSL\incus-certs\incus-smoke ./cmd/incus-smoke
wsl -d Incus -u root -- bash -c "REMOTE_URL=https://localhost:8443 CLIENT_CERT=/root/lahijan-client.crt CLIENT_KEY=/root/lahijan-client.key /mnt/e/WSL/incus-certs/incus-smoke"
```

## Troubleshooting

| Symptom | Fix |
|---|---|
| `listen vsock …: function not implemented` | You're inside Docker, or the WSL kernel is old. Use a WSL2 distro; `wsl --update` + `wsl --shutdown`. |
| `Failed to check dnsmasq version` at `incus admin init` | `apt install dnsmasq-base` |
| `No root device could be found` at launch | `incus profile device add default root disk path=/ pool=default` |
| `Network "incusbr0" unavailable` | mirrored networking; remove `eth0` from the profile or switch to NAT |
| `image couldn't be found` in <1s | wrong alias — use `images:ubuntu/24.04` (not `images:alpine/3.20` / `images:busybox`) |
| Image download stalls ~0 B/s | NAT mode + localhost proxy → switch to `networkingMode=mirrored` |
| Ping → 404 on `/1.0/` | (already fixed in `client.go`) — don't reintroduce the trailing-slash URL builder |
| `wsl: Failed to start the systemd user session for 'root'` | harmless (user-session warning, not the system daemon) |

## Cleanup

```powershell
wsl --shutdown
wsl --unregister Incus          # removes the distro + its vhdx
Remove-Item -Recurse E:\WSL\Incus, E:\WSL\incus-certs
```
