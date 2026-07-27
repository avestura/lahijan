# Lahijan — Incus topology

This directory holds the preseed template applied to the Incus daemon
that serves Lahijan's compute module (WS-11 / WS-14).

Per **ADR-0040** the canonical topology is **containerized Incus**: the
daemon runs as a privileged service in the Lahijan compose stack itself.
The legacy host-installed-Incus path still works and is documented at
the bottom of this file for operators who cannot accept the privileged
constraints.

---

## Containerized Incus (default — per ADR-0040)

Both `docker-compose.dev.yml` and `docker-compose.prod.yml` ship a
top-level `incus` service:

```yaml
incus:
  image: ghcr.io/cmspam/incus-docker:${INCUS_IMAGE_TAG:-lts}
  privileged: true
  network_mode: host
  pid: host
  cgroup: host
  environment:
    SETIPTABLES: "true"
    KVM_GID: ${INCUS_KVM_GID:-}
  volumes:
    - /dev:/dev
    - /var/lib/incus:/var/lib/incus   # prod: host bind-mount; dev: named volume `incus_data`
    - /lib/modules:/lib/modules:ro
```

The image is **officially referenced** in the Incus install docs
(<https://linuxcontainers.org/incus/docs/main/installing/>, Debian →
Docker section). The `:lts` tag is Incus 6.0 LTS (Debian-based,
VM-capable via QEMU).

### Why the privileged flags are non-negotiable

Incus is a container/VM manager. Running it inside a container requires
the same kernel access the daemon has on a host:

- **`privileged: true`** — needed to call `socket(AF_VSOCK)`, manage
  cgroups, create network bridges, and hand off `/dev/kvm` for VMs.
- **`network_mode: host`** — Incus creates its own `incusbr0` bridge
  and bridges instance traffic; it cannot do this from inside a
  bridge-networked container without nested networking.
- **`pid: host`** — required to fix the cgroup cpuset error
  (`balance: Unable to set cpuset`) per the cmspam README.
- **`cgroup: host`** (Compose v2 spelling of `--cgroupns=host`) —
  cgroup v2 unified hierarchy.
- **`SETIPTABLES=true`** — inserts `iptables -I DOCKER-USER -j ACCEPT`
  so `incusbr0` traffic is not blocked by Docker's iptables rules.

There is no way to run Incus unprivileged inside a container.

### How Lahijan reaches the daemon

The Lahijan container mounts `/var/lib/incus` **read-only** and dials
`/var/lib/incus/unix.socket` via `NewUnixClient`
([`internal/app/lahijan/providers/incus/client.go:213`](../../internal/app/lahijan/providers/incus/client.go)).
No mTLS, no cert generation, no published port. The Incus socket is
never exposed on the network — Unix-only.

### Initialize the daemon (one-time, post `compose up`)

```bash
# dev stack
docker compose -f deployments/docker-compose.dev.yml exec incus \
  incus admin init --preseed < /dev/stdin < deployments/incus/preseed.yaml

# prod stack
docker compose -f deployments/docker-compose.prod.yml exec incus \
  incus admin init --preseed < /dev/stdin < deployments/incus/preseed.yaml
```

### Verify

```bash
docker compose -f deployments/docker-compose.dev.yml exec incus incus list
# +------+-------+------+------+------+-----------+
# | NAME | STATE | IPV4 | IPV6 | TYPE | SNAPSHOTS |
# +------+-------+------+------+------+-----------+

docker compose -f deployments/docker-compose.dev.yml exec incus incus launch images:ubuntu/24.04 test1
docker compose -f deployments/docker-compose.dev.yml exec incus incus exec test1 -- echo alive
```

### Storage / state

- **Prod:** `/var/lib/incus` is bind-mounted from the host so an
  operator's host-installed `incus` CLI manages the same daemon Lahijan
  talks to. The state survives `docker compose down` (without `-v`).
- **Dev:** `/var/lib/incus` is the named volume `incus_data`
  (`lahijan-incus-data-dev`) so the stack works identically on any
  Docker host, including Docker Desktop for Mac.

### Host requirements

The host kernel must support the modules Incus needs. Most mainstream
Linux distros ship them loaded by default; minimal hosts (ClearLinux,
RHEL CoreOS) sometimes don't.

```bash
# Verify
ls /sys/module/vhost_vsock /sys/module/veth /sys/module/bridge 2>/dev/null

# Load (persist across reboots)
echo -e "vhost_vsock\nveth\nbridge" > /etc/modules-load.d/incus.conf
modprobe vhost_vsock veth bridge
```

For VMs (instances of type `--vm`), `/dev/kvm` must be present:

```bash
ls -l /dev/kvm
```

### Optional: AppArmor passthrough

On OpenSUSE Tumbleweed / RHEL / Fedora where AppArmor is enforcing, you
may need to grant dnsmasq write access to `/var/lib/incus/**`. Edit
`/etc/apparmor.d/usr.sbin.dnsmasq`, find the line
`/var/log/dnsmasq*.log w,`, and add below it:

```
/var/lib/incus/** rw,
```

You can also pass AppArmor through to the container by uncommenting the
`/sys/kernel/security:/sys/kernel/security` mount in the compose file.

### Optional: KVM GID for shared virtualization hosts

If you run libvirt / qemu on the host (RHEL, CoreOS, Fedora), the
container's internal udev may re-chown `/dev/kvm` and break host VMs.
Find the host KVM GID and pass it through:

```bash
getent group kvm | cut -d: -f3   # e.g. 36 on RHEL
```

```env
# .env.prod
INCUS_KVM_GID=36
```

---

## Host install (advanced / legacy)

This is the supported fallback for:

- Operators who cannot accept `--privileged --network host --pid host`
  for policy reasons (SELinux enforcing, read-only root, hardened
  container policy).
- Windows / macOS dev hosts where the containerized Incus fails on
  Docker Desktop (see ADR-0040 Risks).

### Setup

1. Install Incus per <https://linuxcontainers.org/incus/docs/main/installing/>.
2. Initialise the daemon with the preseed **on the host**:

   ```bash
   incus admin init --preseed < deployments/incus/preseed.yaml
   ```

3. Grant the user the Lahijan app runs as the `incus-admin` group, or
   use a dedicated `lahijan` system user with membership in the incus
   group.
4. Confirm `incus list` works as that user before bringing up the stack.

### Wiring Lahijan to a host-installed Incus

Remove the `incus` service from the compose override file (or use a
`docker-compose.override.yml` to null it out) and re-add the legacy
bind-mount on the Lahijan container:

```yaml
services:
  incus:
    profiles: ["host-incus-disabled"]   # never started

  lahijan:
    volumes:
      - /var/lib/incus:/var/lib/incus:ro
```

Lahijan picks up the socket via the same `NewUnixClient` path; no code
change.

### Windows host fallback

If `docker compose up incus` fails with `socket: function not implemented`
(Docker Desktop's VM blocked `AF_VSOCK`), use the WSL2-based path
documented in:

- `docs/howto/run-incus-on-windows.md`
- `.opencode/skills/incus-on-windows/SKILL.md`
- `scripts/Setup-Incus.ps1` — automated installer

The WSL2 distro has a real Linux kernel where AF_VSOCK works; Lahijan
reaches the daemon over HTTPS+mTLS (`providers.incus.remoteURL`).

---

## Preseed reference

See [`preseed.yaml`](./preseed.yaml) for the cluster-wide primitives
(default bridge `lahijanbr`, dir-backed `default` storage pool, fallback
`default` profile). Per-tenant Incus projects are created at tenant
bootstrap by the compute module (WS-14), NOT here.
