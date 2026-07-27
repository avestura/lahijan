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

## Step 0 (try this first): the containerized Incus

Per **ADR-0040** the canonical topology on every platform is the
containerized Incus in `docker-compose.dev.yml`:

```powershell
docker compose -f deployments/docker-compose.dev.yml up -d incus
docker compose -f deployments/docker-compose.dev.yml logs incus | Select-String vsock
docker compose -f deployments/docker-compose.dev.yml exec incus incus list
```

If `incus list` returns an empty table, you are done — skip the rest of
this file. The sections below are the **fallback** for the case where
the containerized Incus fails on Docker Desktop.

## The hard rule about AF_VSOCK (fallback rationale)

Incus's daemon fatality-binds a **vsock listener** at startup
(`listen vsock host(2):NNNN`). If `socket(AF_VSOCK)` returns `ENOSYS`,
the daemon dies with `socket: function not implemented`.

| Environment | AF_VSOCK | Incus? |
|---|---|---|
| **WSL2 distro** (kernel `*-microsoft-standard-WSL2`, 6.x) | ✅ works (`/dev/vhost-vsock` present) | ✅ runs |
| **Docker container** on Docker Desktop Windows | ❌ `ENOSYS` (verified, see below) | ❌ fatal |
| Windows native | ❌ no Linux kernel | ❌ client only |

**Verified empirically on Docker Desktop 28.1.1 (Jul 2026):** a
`--privileged` container with `/dev:/dev` bind-mounted DOES see the
`/dev/vsock` device (10:259), but `socket(AF_VSOCK, SOCK_STREAM, 0)`
returns `ENOSYS`. The Docker FAQ's mention of "AF_VSOCK hypervisor
sockets" describes Docker Desktop's *own* backend↔VM IPC, not access
exposed to containers. So the cmspam Incus image will fail on every
Docker Desktop release tested to date.

The Lahijan stack **still tries the container first** (Step 0) — Docker
Desktop may lift this restriction in a future release, and the check
is cheap. When it fails (and it will, today), fall back to the WSL2
path below.

## Verify AF_VSOCK before you start (WSL2 fallback path)

```bash
wsl -d <Distro> -u root -- bash -c "ls -la /dev/vhost-vsock && python3 -c 'import socket; socket.socket(40,1)' 2>&1 || echo 'AF_VSOCK missing'"
```

If `/dev/vhost-vsock` is missing or the socket call fails on a recent
kernel, run `wsl --update` then `wsl --shutdown` (older WSL shipped a
kernel without vsock — fixed in current releases).

## The reliable WSL2 bring-up (no admin, no Store)

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

`scripts\Setup-Incus.ps1` also installs the Incus CLI on Windows via
`winget install LinuxContainers.Incus` and wires it to the WSL2 daemon
so `incus list` works from PowerShell/cmd directly (no `wsl -d Incus`
prefix needed). The wiring lives at `%APPDATA%\incus\`:

- `client.crt` + `client.key` — global client cert (daemon already trusts it)
- `servercerts\wsl-incus.crt` — daemon's server cert (no fingerprint prompt)
- `config.yml` — remote `wsl-incus` set as default

If `incus list` from Windows fails but `wsl -d Incus -u root -- incus list`
works, the wiring is broken — re-run `Setup-Incus.ps1` (idempotent).

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

3. **Incus 6.0 LTS renamed the project-restriction keys.** The pre-6.0
   `restrict` master toggle was renamed to `restricted`, and
   `restricted.devices.unix` was split into `restricted.devices.unix-block`
   + `restricted.devices.unix-char`. The pre-6.0 values are silently
   accepted by the fake but cause HTTP 400 on a real 6.0 daemon at
   project-create time. Always verify restricted-defaults keys against
   `incus project set default <key>=test` on the target daemon version
   before trusting the schema.

4. **`restricted.networks.subnets` rejects the literal `"block"`.** The
   value must be a comma-separated list of `<uplink>:<subnet>` entries
   (or empty). Setting it to `"block"` (the value used by every other
   `restricted.*` key) is rejected. Block subnet allocation via
   `restricted.networks.uplinks=block` instead — the subnet key becomes
   irrelevant when there's no uplink to allocate on.

5. **Duplicate-profile inserts return HTTP 500 (not 409) in Incus 6.0.**
   The DB-layer INSERT path raises `"The profile already exists"` with a
   500 status code, not the 409 Conflict the REST spec implies. The
   driver's `classify()` only matched `StatusConflict + "already exists"`;
   idempotent upserts (`EnsureProfile`, `EnsureProject`) silently failed
   against the real daemon while passing against the well-behaved fake.
   The driver now treats any 5xx with `"already exists"` in the message
   as `ErrAlreadyExists`. Apply the same pattern to any other
   "expected conflict the daemon reports as 5xx" path you encounter.

6. **The async-operation wait endpoint returns HTTP 200 + `type:"error"`
   when the operation failed.** Incus' POST `/1.0/instances` returns
   202 + an operation URL; the daemon then runs the actual create
   asynchronously. `GET /1.0/operations/<id>/wait` returns HTTP 200 in
   BOTH cases — success returns `type:"async"` + the Operation, failure
   returns `type:"error"` + `error_code:500` + an error message. A
   decoder that only looks for the Operation struct silently swallows
   the failure and returns an empty Operation. The driver now probes
   the envelope `type` first and surfaces `type:"error"` as a Go error.

7. **`POST /1.0/instances` routes by the `?project=<name>` query
   parameter, NOT by the `project` field in the JSON body.** Without
   the query parameter, the create silently lands in the daemon's
   `default` project regardless of what the body carries — every
   subsequent tenant-scoped call (which DOES pass `?project=`) fails to
   find the instance. The body field is decorative. The same is true
   for profiles, networks, and storage volumes — always check whether
   the endpoint takes its scope from the query string or the body; the
   fake accepts both.

8. **A pool-less `root` device in the instance create body overrides
   the profile's correctly-pooled root device and is rejected.** The
   dashboard sends `devices.root={type:disk, path:/, size:10GiB}` with
   no `pool`. Incus' device-merging logic treats the instance-level
   device as authoritative and rejects it with `"Root disk entry must
   have a 'pool' property set"`. Either include `pool=<name>` in the
   request (which requires querying the daemon for its first pool) or
   drop the device and let the seeded default profile (see #9) provide
   it. Lahijan does the latter in `compute/instances.go:CreateInstance`.

9. **A freshly-created project's `default` profile starts empty.**
   Without a root disk in the profile, every instance create fails with
   `"No root device could be found"` (real daemon) while passing
   against the fake (which pre-seeds a usable project). `EnsureProject`
   now seeds the default profile with `devices.root={type:disk, path:/,
   pool=<first-storage-pool>}` on BOTH the create-new and
   update-existing paths so the seed is truly idempotent. NICs are NOT
   seeded — `restricted.devices.nic=managed` + `features.networks=true`
   means the project can't see the daemon's bridges; networking is
   configured per-instance via the UI/API.

10. **Image aliases without `source.server` look up locally only.** A
    fresh tenant project has no cached image aliases (the cached image
    lands under its fingerprint only), so a create with
    `source={type:"image", alias:"ubuntu/24.04"}` fails silently. The
    compute service now auto-populates `source.server=
    https://images.linuxcontainers.org` + `source.protocol=simplestreams`
    when the alias contains `/` and no explicit fingerprint is set.

**Lesson:** any new driver endpoint should be smoke-tested against a real
Incus at least once — the fake lets schema/routing/protocol drift
through. The temporary cross-compiled smoke binary pattern (in the howto
doc) is the fastest way to do this without running the full Lahijan
backend. When you do find a fake-vs-real gap, **extend the fake** to
model the real behaviour (so future tests catch the drift) AND add the
gotcha to this list.

## Quick triage map

| Symptom | Cause | Fix |
|---|---|---|
| Containerized incus service fails (`docker compose up incus`) | Expected on Docker Desktop — AF_VSOCK is blocked even for privileged containers | Follow the WSL2 path (Option A in the howto) |
| `listen vsock ...: function not implemented` inside WSL2 | AF_VSOCK unavailable (old WSL kernel) | `wsl --update` + `wsl --shutdown` |
| `Failed to check dnsmasq version` at init | `dnsmasq-base` not installed | `apt install dnsmasq-base` |
| `No root device could be found` at launch | tenant project's default profile is empty (no root disk) | `EnsureProject` auto-seeds `devices.root` on the default profile; if you bypassed it, run `incus profile device add default root disk path=/ pool=default --project lahijan-tenant-<uuid>` |
| `Invalid project configuration key "restrict"` (or `"restricted.devices.unix"`) | Incus 6.0 renamed the schema; pre-6.0 keys are rejected | Use `restricted=true`, `restricted.devices.unix-block=block`, `restricted.devices.unix-char=block` (see pitfall #3) |
| `Invalid project configuration key "restricted.networks.subnets" value: Subnet "block" invalid` | Incus 6.0 expects `<uplink>:<subnet>` format for this key | Drop the key entirely; `restricted.networks.uplinks=block` already prevents subnet allocation (see pitfall #4) |
| `incus: http 500: Error inserting "default" into database: The profile already exists` | Incus 6.0 returns 500 (not 409) for duplicate inserts; idempotent upserts that only check 409 silently fail | Driver now treats any 5xx with `"already exists"` as `ErrAlreadyExists` (see pitfall #5) |
| Audit log says `compute.instance.create` success but `incus list` (in the tenant project) shows nothing | The POST was missing the `?project=<name>` query; instance landed in the `default` project | Driver now adds `?project=` to the create POST (see pitfall #7) |
| `Root disk entry must have a "pool" property set` | Dashboard sent `devices.root` without `pool`; instance device overrode the profile's | Either include `pool=<name>` or drop the device so the profile's root disk applies (see pitfall #8) |
| Audit says success + instance exists in DB but image never appears / pull never starts | `source.alias` had no `source.server`; daemon looked up locally only and the project had no cached alias | Compute service auto-fills `source.server=https://images.linuxcontainers.org` when alias contains `/` (see pitfall #10) |
| `Network "incusbr0" unavailable` | mirrored networking mode | remove `eth0` from profile, or switch to NAT |
| `image couldn't be found` in <1s | wrong alias (not a network issue) | use `images:ubuntu/24.04`; check `incus image list` |
| Image download stalls at 0 B/s | NAT mode + localhost proxy | `.wslconfig` `networkingMode=mirrored` + `wsl --shutdown` |
| Ping → 404 `/1.0/` | driver trailing-slash bug | fixed in `client.go` (don't reintroduce) |
| Windows → `localhost:8443` refused | mirrored loopback not forwarded | `[experimental] hostAddressLoopback=true` |
| `incus remote add` ignores existing client cert + asks for trust token | Incus 7.x changed the trust flow; `remote add` regenerates the cert | Don't use `remote add`. Edit `%APPDATA%\incus\config.yml` directly + drop the cert files in place — `Setup-Incus.ps1` does this for you |
| `incus list` from Windows shows nothing / "remote doesn't exist" | Wiring is in the wrong dir (NOT `~/.config/incus/`) | Re-run `Setup-Incus.ps1`; the Windows Incus CLI reads from `%APPDATA%\incus\` (verified Incus 7.2) |
| Zabbly apt "no Release file" + curl works | old GnuTLS (focal) | use noble (24.04), not focal |

## References

- Full contributor walkthrough: `docs/howto/run-incus-on-windows.md`
- Automated setup: `scripts/Setup-Incus.ps1`
- Windows dev launcher (app + frontends): `scripts/Run-Dev.ps1`
- Driver source: `internal/app/lahijan/providers/incus/`
- Config keys: `providers.incus.*` in `internal/app/lahijan/conf/.lahijan.conf.default.yaml`
