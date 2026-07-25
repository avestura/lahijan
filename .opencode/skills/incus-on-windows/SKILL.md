---
name: incus-on-windows
description: "Use when bringing up a REAL Incus daemon for local Lahijan dev on a Windows host, or when debugging Incus/WSL2 networking, vsock errors, or the Incus driver talking to a real daemon. Triggers on 'incus on windows', 'run incus locally', 'vsock function not implemented', 'incusbr0 unavailable', 'wsl incus', 'mirrored networking', Incus driver Ping 404, or setting up compute dev on Windows."
---

# Running a real Incus on Windows for Lahijan dev

Lahijan's compute module (WS-14) talks to Incus. The in-process fake
(`providers/incus/fake/`) is enough for unit tests, but it does NOT catch
wire-level mismatches between the driver and a real daemon — two real
driver bugs were found this way (see "Driver pitfalls" below). This skill
is the shortest path to a real Incus running on a Windows dev host.

## The one hard rule: Incus needs AF_VSOCK, and only WSL2 has it

Incus's daemon fatality-binds a **vsock listener** at startup
(`listen vsock host(2):NNNN`). If `socket(AF_VSOCK)` returns `ENOSYS`,
**every** Incus build (Zabbly / `ghcr.io/cmspam/incus-docker` / stock)
dies with `socket: function not implemented`.

| Environment | AF_VSOCK | Incus? |
|---|---|---|
| **WSL2 distro** (kernel `*-microsoft-standard-WSL2`, 6.x) | ✅ works (`/dev/vhost-vsock` present) | ✅ runs |
| **Docker container** on Docker Desktop Windows | ❌ `ENOSYS` (Docker Desktop's VM blocks it) | ❌ fatal |
| Windows native | ❌ no Linux kernel | ❌ client only |

**Do NOT waste time on the Docker/Podman Incus images on Windows.** They
are designed for real Linux hosts and die on Docker Desktop. Use a WSL2
distro. (If you see the cmspam image suggested, that's for Linux hosts.)

## Verify AF_VSOCK before you start

```bash
wsl -d <Distro> -u root -- bash -c "ls -la /dev/vhost-vsock && python3 -c 'import socket; socket.socket(40,1)' 2>&1 || echo 'AF_VSOCK missing'"
```

If `/dev/vhost-vsock` is missing or the socket call fails on a recent
kernel, run `wsl --update` then `wsl --shutdown` (older WSL shipped a
kernel without vsock — fixed in current releases).

## The reliable bring-up (no admin, no Store)

`scripts/Setup-Incus.ps1` automates all of this. The manual shape:

1. **Import Ubuntu 24.04** as a WSL2 distro WITHOUT admin/Store:
   ```powershell
   docker pull ubuntu:24.04
   docker create --name noble-rootfs ubuntu:24.04 true
   docker export noble-rootfs -o E:\WSL\noble-rootfs.tar
   docker rm noble-rootfs
   wsl --import Incus E:\WSL\Incus E:\WSL\noble-rootfs.tar --version 2
   ```
   The Store `ubuntu:24.04` image is minimal — it has NO systemd and NO
   `kmod`/`python3`. You must `apt install systemd systemd-sysv kmod dbus`.
   `ubuntu:24.04` (noble) is the minimum Zabbly supports; **focal (20.04)
   is too old** (Zabbly has no focal release, and focal's GnuTLS can't
   complete the TLS handshake with pkgs.zabbly.com).

2. **Enable systemd** via `/etc/wsl.conf`:
   ```ini
   [boot]
   systemd=true
   [user]
   default=root
   ```
   Then `wsl --terminate Incus` and re-enter. PID 1 must be `systemd`.
   (A cosmetic `Failed to start the systemd user session for 'root'`
   warning is normal — it's the user session, not the system daemon.)

3. **Install Incus** via Zabbly (noble):
   ```bash
   curl -fsSL https://pkgs.zabbly.com/key.asc -o /etc/apt/keyrings/zabbly.asc
   echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/zabbly.asc] https://pkgs.zabbly.com/incus/stable noble main" > /etc/apt/sources.list.d/zabbly.list
   apt update && apt install -y --no-install-recommends incus dnsmasq-base
   ```
   `--no-install-recommends` skips `dnsmasq-base` — **install it explicitly**
   or `incus admin init --auto` fails creating `incusbr0`.

4. **Initialize**:
   ```bash
   incus admin init --auto
   incus profile device add default root disk path=/ pool=default
   ```
   The `--auto` preseed sometimes doesn't add the profile's root disk if
   init fails midway (e.g. on missing dnsmasq) — verify with
   `incus profile show default`.

5. **Launch**:
   ```bash
   incus launch images:ubuntu/24.04 test1
   incus exec test1 -- echo alive
   ```

## Networking: the mirrored-vs-NAT trade-off (read this)

WSL2 networking has a painful either/or on Windows hosts that have a
**localhost proxy** (VPN, corporate, dev tool):

| `.wslconfig` `networkingMode` | Image downloads | `incusbr0` bridge (container IPs) |
|---|---|---|
| `mirrored` | ✅ fast | ❌ "Network unavailable" (can't create bridge) |
| `nat` (default) | ❌ choked (~0 B/s when a localhost proxy is present) | ✅ works |

The warning `A localhost proxy configuration was detected but not mirrored
into WSL. WSL in NAT mode does not support localhost proxies.` is the
tell. Mirrored mode fixes downloads but Incus **cannot create the Linux
bridge** (`incusbr0` stays UNAVAILABLE, not in the kernel). So:

- **For compute dev (create/start/exec)** — mirrored is fine. The Lahijan
  driver talks to the Incus API, not the container network. Containers
  run without an IP; remove `eth0` from the default profile to launch:
  `incus profile device remove default eth0`.
- **For container networking (instances must have IPs)** — you need NAT
  mode AND no localhost proxy on the Windows side. Or use `nictype:
  physical`/`macvlan` on the host NIC.

## Wiring Lahijan's driver to this Incus

Lahijan-on-Windows reaches Incus over **HTTPS + mTLS** (the unix socket
isn't reachable from Windows). Incus listens on `:8443`; the client
cert must be trusted.

```bash
# inside the Incus distro
incus config set core.https_address :8443
openssl req -x509 -newkey rsa:2048 -nodes -keyout lahijan-client.key \
  -out lahijan-client.crt -days 3650 -subj "/CN=lahijan-dev-driver"
incus config trust add-certificate lahijan-client.crt --type=client --name lahijan-dev-driver
```

Then run Lahijan with (CLI flags — Viper env vars don't override the
embedded YAML defaults, see `scripts/Run-Dev.ps1`):

```
--providers.incus.enabled
--providers.incus.socketPath=
--providers.incus.remoteURL=https://localhost:8443
--providers.incus.tls.clientCert=<PEM>
--providers.incus.tls.clientKey=<PEM>
--providers.incus.tls.insecureSkipVerify
```

**Caveat:** Windows→WSL2 `localhost:8443` needs `[experimental]
hostAddressLoopback=true` in `.wslconfig` (mirrored mode) or a firewall
rule; otherwise Windows gets "connection refused". The most reliable
verification is to run the driver inside the distro against
`localhost:8443` (where it always works) — see the smoke binary pattern
in `docs/howto/run-incus-on-windows.md`.

## Driver pitfalls that only surface against a real daemon

These were real production bugs, invisible against the in-process fake
(the fake doesn't enforce routing/typing the way real Incus does):

1. **`Ping` hit `/1.0/` (trailing slash) → real Incus returns 404.**
   The `do()` URL builder appended `"/"` even when the path was empty.
   Real Incus's router 404s `/1.0/`; `/1.0` and `/1.0/instances` are 200.
   Fixed by only appending the separator when there's a sub-path.

2. **`serverInfo.Environment` was `map[string]string` → decode failure.**
   Real Incus returns MIXED types in `environment` (`architectures` is a
   `[]string`, `driver`/`kernel` are strings). Changed to `map[string]any`.

**Lesson:** any new driver endpoint should be smoke-tested against a real
Incus at least once — the fake lets type/routing drift through. The
temporary cross-compiled smoke binary pattern (in the howto doc) is the
fastest way to do this without running the full Lahijan backend.

## Quick triage map

| Symptom | Cause | Fix |
|---|---|---|
| `listen vsock ...: function not implemented` | AF_VSOCK unavailable (you're in Docker, or old WSL kernel) | Use a WSL2 distro; `wsl --update` + `wsl --shutdown` |
| `Failed to check dnsmasq version` at init | `dnsmasq-base` not installed | `apt install dnsmasq-base` |
| `No root device could be found` at launch | default profile missing root disk | `incus profile device add default root disk path=/ pool=default` |
| `Network "incusbr0" unavailable` | mirrored networking mode | remove `eth0` from profile, or switch to NAT |
| `image couldn't be found` in <1s | wrong alias (not a network issue) | use `images:ubuntu/24.04`; check `incus image list` |
| Image download stalls at 0 B/s | NAT mode + localhost proxy | `.wslconfig` `networkingMode=mirrored` + `wsl --shutdown` |
| Ping → 404 `/1.0/` | driver trailing-slash bug | fixed in `client.go` (don't reintroduce) |
| Windows → `localhost:8443` refused | mirrored loopback not forwarded | `[experimental] hostAddressLoopback=true` |
| Zabbly apt "no Release file" + curl works | old GnuTLS (focal) | use noble (24.04), not focal |

## References

- Full contributor walkthrough: `docs/howto/run-incus-on-windows.md`
- Automated setup: `scripts/Setup-Incus.ps1`
- Windows dev launcher (app + frontends): `scripts/Run-Dev.ps1`
- Driver source: `internal/app/lahijan/providers/incus/`
- Config keys: `providers.incus.*` in `internal/app/lahijan/conf/.lahijan.conf.default.yaml`
