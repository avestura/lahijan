# ADR-0040: Incus-in-container topology — privileged `incus` service via `ghcr.io/cmspam/incus-docker`

- **Status:** Accepted
- **Date:** 2026-07-26
- **Deciders:** maintainer
- **Supersedes:** the Incus-placement dimension of [ADR-0005](./0005-mvp-topology.md)
  (the rest of ADR-0005 — single-host topology, multi-host-readiness — stands).

## Context

ADR-0005 (2026-07-17) committed Lahijan to a single-host topology where
**Incus runs on the host, not in a container**, and the Lahijan process
reach it via a bind-mounted Unix socket. That was the conventional
wisdom: Incus needs kernel access (`AF_VSOCK`, cgroups, `/dev/kvm`,
`/lib/modules`, network bridges), and the Docker images that wrapped
Incus were considered brittle.

The cost of that choice landed across three areas:

1. **It breaks the "one-command deploy" promise of ADR-0006.** Operators
   must install Incus separately (`apt install incus`, Zabbly repo,
   snap), run `incus admin init`, then `docker compose up`. Every dev
   and prod bring-up has an out-of-band step that no script can fully
   automate.
2. **The Windows dev story is hostile.** Docker Desktop's VM blocks
   `AF_VSOCK` for containers, so the cmspam image dies there. The
   project carries a 231-line PowerShell script (`scripts/Setup-Incus.ps1`)
   that imports an Ubuntu rootfs into a WSL2 distro, enables systemd,
   installs Incus via Zabbly, exposes `:8443` + mTLS, and prints
   driver flags — and even then it does not yield a happy-path
   end-to-end compute run on the first try. The Lahijan maintainer had
   not, at the time of this ADR, seen a single user-container launched
   by Lahijan succeed without manual intervention.
3. **The Linux prod story is harder than it needs to be.** A fresh VM
   with Docker + Incus hand-installed takes ~1 hour per
   `docs/workstreams/WS-23-production-deployment-ops.md` DoD, and the
   host-install produces a `/var/lib/incus` whose state lives "outside
   the compose stack" (`scripts/restore.sh` comment). Backup scripts
   have to special-case it.

Meanwhile, the upstream Incus project now **explicitly endorses** a
Docker image in its canonical install docs
(<https://linuxcontainers.org/incus/docs/main/installing/>, Debian →
Docker section):

> Docker/Podman images of Incus, based on the Zabbly package
> repository, are available with instructions here:
> `ghcr.io/cmspam/incus-docker`

The image (`ghcr.io/cmspam/incus-docker`) packages the Zabbly-built
Incus + incus-ui-canonical, ships a `:latest` (Debian stable rolling),
`:lts` (6.0 LTS), and `:daily` tag, and documents the exact
`docker run` invocation that exposes everything Incus needs:
`--privileged --network host --pid host --cgroupns host`, plus
bind-mounts for `/dev`, `/var/lib/incus`, and `/lib/modules`.

The Lahijan driver layer is already topology-agnostic. The
`NewUnixClient` constructor
([`internal/app/lahijan/providers/incus/client.go:213`](../../internal/app/lahijan/providers/incus/client.go))
dials a Unix socket; `NewRemoteClient` (`client.go:234`) does
HTTPS+mTLS; the selection happens in `program/incus_provider.go:89-106`
on whether `providers.incus.remoteURL` is empty. Switching the canonical
endpoint is a config flip, not a code change. The in-process fake
(`providers/incus/fake/`) plus the test sandbox (ADR-0029) keep the
test path entirely decoupled from any daemon placement.

## Options considered

### A. Status quo — host-installed Incus, sidecar `incus-client` mounts socket

- **Pros:** the existing topology; nothing to migrate; minimal
  privileges for the Lahijan container itself.
- **Cons:** breaks ADR-0006; the Windows dev story stays hostile; prod
  bring-up has out-of-band steps; Incus state lives outside the
  compose-managed volume set.

### B. Containerized Incus via `ghcr.io/cmspam/incus-docker:lts` (chosen)

- **Pros:** true one-command deploy; Incus state in a managed volume
  (backup story simplifies); identical dev + prod topology; the
  upstream-endorsed path; Windows dev *may* work too (depends on
  Docker Desktop's `AF_VSOCK` exposure — see Risks).
- **Cons:** the `incus` container must run `--privileged --network host
  --pid host --cgroup host`; some hosts (RHEL/CoreOS with AppArmor or
  SELinux) need extra module-load + policy tweaking; AF_VSOCK is not
  guaranteed on Docker Desktop for Windows/Mac.

### C. Containerized Incus via the official `ghcr.io/lxc/incus:6` image

- **Pros:** canonical source; smaller image.
- **Cons:** the upstream image ships the daemon but **not** the
  opinionated entrypoint that handles iptables (`SETIPTABLES=true`),
  `/dev/kvm` GID remapping (`KVM_GID`), module loading, and AppArmor
  passthrough. Every Linux-distro corner case the cmspam image solves
  becomes our problem. The Incus install docs point operators at the
  cmspam image, not the bare `lxc/incus` image, for the same reason.

### D. Hybrid — sidecar-with-socket OR containerized, operator picks

- **Pros:** maximum flexibility.
- **Cons:** doubles the support matrix; both topologies have to be
  tested. Operators rarely benefit from the choice — they want one
  obvious path.

## Decision

Lahijan ships **containerized Incus as the canonical topology**, in
both `docker-compose.dev.yml` and `docker-compose.prod.yml`:

1. **Image:** `ghcr.io/cmspam/incus-docker:lts` (Incus 6.0 LTS,
   Debian-based, VM-capable via QEMU). Pinned to a SemVer tag per the
   prod hard rule ("never use `latest`"). Dev uses the same tag for
   parity.

2. **Container flags (both stacks):**
   - `privileged: true`
   - `network_mode: host` — Incus creates `incusbr0` and bridges
     instance traffic; it cannot do this from inside a bridge-network
     container without nested networking.
   - `pid: host` — required to fix cgroup cpuset errors (per the cmspam
     README: *"balance: Unable to set cpuset" without it*).
   - `cgroup: host` (Compose v2 spelling of `--cgroupns=host`) — cgroup
     v2 unified hierarchy.
   - `environment.SETIPTABLES: "true"` — inserts
     `iptables -I DOCKER-USER -j ACCEPT` so `incusbr0` traffic is not
     blocked by Docker's iptables rules.
   - Volume mounts: `/dev:/dev`, `/lib/modules:/lib/modules:ro`, and
     the Incus state directory.

3. **State storage:**
   - **Dev:** a named compose volume (`incus_data`) mounted at
     `/var/lib/incus`. Works on any Docker host (including Docker
     Desktop on Windows/Mac, modulo AF_VSOCK availability).
   - **Prod:** a host bind-mount `/var/lib/incus:/var/lib/incus` (per
     the cmspam README). Operators who already have Incus installed on
     the host can keep their state in place.

4. **Lahijan→Incus transport: Unix socket at
   `/var/lib/incus/unix.socket` via the shared volume.** No mTLS, no
   cert generation. The Lahijan container mounts the same `incus_data`
   volume (or the same host path) read-only at `/var/lib/incus` and
   dials the socket with `NewUnixClient` — exactly the path the driver
   already supports. `providers.incus.remoteURL` stays empty.

5. **The `incus-client` sidecar service is removed from both compose
   files.** It existed only to fail-fast at startup if the host socket
   was missing; with Incus in the same compose stack, the new `incus`
   service's own healthcheck covers that role.

6. **Healthcheck:** every Incus container ships a HEALTHCHECK that
   probes `incus list` (preferred) and falls back to the socket file.
   Cold starts allow 60 seconds for the daemon to come up + apply any
   DB schema upgrade.

7. **The host-install path stays documented but de-preferred.** It
   lives in `deployments/incus/README.md` under "Host install
   (advanced/legacy)" and is the supported fallback for:
   - Windows/Mac hosts where the containerized Incus fails on Docker
     Desktop (AF_VSOCK issue — see Risks).
   - Operators who cannot accept `--privileged --network host --pid
     host` for policy reasons.

## Consequences

- **Positive:** `docker compose up` is the only step. The ADR-0006
  promise of "Lahijan ships the whole stack" is finally literal.
- **Positive:** dev and prod topologies are identical — operators debug
  the same layout they ship.
- **Positive:** the Incus daemon's state (`/var/lib/incus`) lives in
  the compose-managed volume set. `scripts/backup.sh` and
  `scripts/restore.sh` can treat it like any other stateful service;
  no more "lives outside the compose stack" caveat.
- **Positive:** the Lahijan driver layer is unchanged. `NewUnixClient`
  was already the default constructor; no code in
  `internal/app/lahijan/providers/incus/` needs editing.
- **Positive:** the in-process fake and ADR-0029 test sandbox are
  unaffected. The containerized Incus is purely a deployment concern.
- **Negative:** the `incus` container runs privileged with host
  networking + PID namespace. This is the price of running a
  container/VM manager inside a container. Operators on hardened hosts
  (SELinux enforcing, read-only root, no `--privileged` policy) must
  use the legacy host-install path.
- **Negative:** the Windows dev story is not fully solved. Docker
  Desktop's VM may or may not expose `AF_VSOCK` to privileged
  containers; the existing skill claims it does not, the Docker
  security FAQ suggests it might. We commit to verify empirically and
  keep `scripts/Setup-Incus.ps1` (WSL2 import) as the documented
  fallback.
- **Negative:** operators who upgrade from an earlier Lahijan version
  must remove their old `incus-client` sidecar and accept the new
  privileged service. The migration is documented in
  `deployments/incus/README.md` and called out in the upgrade notes.

## Compliance

- `deployments/docker-compose.dev.yml` defines a top-level `incus`
  service with the flags in §2 and a named volume `incus_data`.
- `deployments/docker-compose.prod.yml` defines a top-level `incus`
  service with the flags in §2 and a host bind-mount of
  `/var/lib/incus`.
- The `incus-client` sidecar is removed from both files.
- The Lahijan `lahijan` service in `docker-compose.prod.yml` depends on
  `incus: service_healthy` and mounts the shared Incus state
  read-only at `/var/lib/incus`.
- `internal/app/lahijan/conf/.lahijan.conf.default.yaml`
  `providers.incus.socketPath` default is `/var/lib/incus/unix.socket`
  (the package layout the cmspam image uses; not the snap layout).
- `deployments/AGENTS.md` adds a hard rule stating the privileged
  constraints and pointing dissenting operators at the host-install
  fallback.
- `deployments/incus/README.md` documents the container topology as
  canonical and the host-install path as "advanced/legacy."
- `scripts/install.sh` no longer requires `command -v incus` on the
  host.
- `scripts/backup.sh` + `scripts/restore.sh` include the Incus
  volume in the snapshot set.

## Risks

1. **AF_VSOCK on Docker Desktop.** Incus fatality-binds a vsock
   listener at startup. The current `incus-on-windows` skill asserts
   this fails for every Docker-based image on Docker Desktop; the
   Docker security FAQ's mention of AF_VSOCK for host↔VM
   communication is about Docker Desktop's *own* IPC, not about
   exposure to containers. The implementation phase verifies
   empirically. If it fails, the WSL2 path
   (`scripts/Setup-Incus.ps1`) remains supported.
2. **AppArmor/SELinux.** On OpenSUSE Tumbleweed, RHEL, CoreOS, and
   Fedora, the privileged container's internal udev may clash with
   AppArmor policies for dnsmasq, or with `/dev/kvm` GID ownership.
   The cmspam README documents the `KVM_GID` env workaround and the
   AppArmor rule addition; `deployments/incus/README.md` cites both.
3. **Kernel modules.** `vhost_vsock`, `kvm`, `veth`, `bridge` must be
   loadable on the host. Minimal hosts (ClearLinux, RHEL CoreOS)
   sometimes lack `vhost_vsock`; the operator must `modprobe` it or
   add it to `/etc/modules-load.d/`. Documented under host
   requirements.
4. **VM support.** The `:lts` and `:latest` tags are Debian-based and
   include QEMU, so instances of type `--vm` work. The `:alpine-novm`
   variant does NOT — operators must not switch to it without an
   explicit exception. The compose files pin `:lts` to prevent
   accidental drift.
5. **`network_mode: host`.** The `incus` container shares the host's
   network namespace, so other compose services cannot reach it via
   the service name `incus:port`. The Lahijan container reaches it
   via the Unix socket on the shared volume, not via TCP. This is why
   the chosen transport is the socket, not HTTPS-over-compose-network.

## References

- [Incus install docs — Debian Docker section](https://linuxcontainers.org/incus/docs/main/installing/)
- [cmspam/incus-docker README](https://github.com/cmspam/incus-docker)
- [ADR-0005](./0005-mvp-topology.md) — superseded on the Incus-placement dimension only
- [ADR-0006](./0006-managed-dependencies.md) — Lahijan ships the whole stack
- [ADR-0010](./0010-full-incus-surface.md) — full Incus API surface (silent on daemon placement; this ADR fills that gap)
- [ADR-0025](./0025-incus-client-library-choice.md) — thin internal REST client
- [ADR-0029](./0029-test-sandbox-topology.md) — test sandbox uses the in-process fake; unaffected
- [ADR-0030](./0030-production-deployment-topology.md) — prod compose is canonical
- `internal/app/lahijan/providers/incus/client.go` — `NewUnixClient` (line 213), `NewRemoteClient` (line 234)
- `internal/app/lahijan/program/incus_provider.go` — transport selection (lines 89-106)
- `deployments/incus/README.md` — operator guide
- `.opencode/skills/incus-on-windows/SKILL.md` — Windows dev fallback
