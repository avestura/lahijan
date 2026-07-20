# `/init` — bare-metal service files

This directory holds systemd unit files for running the Lahijan binary
directly on a host (no Docker). It is **not the recommended install path** —
the docker-compose stack at `deployments/docker-compose.prod.yml` is the
canonical form for MVP (per ADR-0005 + ADR-0006).

## When to use this

- A homelab operator who wants Lahijan running on bare metal without
  Docker (advanced — you'll need to wire Postgres, PowerDNS, and
  SeaweedFS yourself).
- A development box where `make run` is too tedious and you want
  Lahijan to start on boot.
- A packaging effort (RPM / Debian / Homebrew) that bundles Lahijan
  as a system service.

For everyone else, **use `docker compose up -d`**:

```bash
cd /opt/lahijan
docker compose --env-file deployments/.env.prod \
  -f deployments/docker-compose.prod.yml up -d
```

The compose stack manages restart-on-failure, logging, and inter-service
wiring automatically. The systemd unit below is a thin alternative.

## Files

- `lahijan.service` — systemd unit that runs `lahijan serve` as the
  `lahijan` system user.
- `install-service.sh` — installs the unit + creates the user + enables
  the service.

## Install (bare-metal)

```bash
sudo ./init/install-service.sh
sudo systemctl enable --now lahijan
sudo journalctl -u lahijan -f
```

The unit assumes the binary is at `/usr/local/bin/lahijan` and the config
file is at `/etc/lahijan/lahijan.yaml`. Adjust the unit if your paths
differ.

## Uninstall

```bash
sudo systemctl disable --now lahijan
sudo userdel lahijan
sudo rm -rf /etc/lahijan /var/lib/lahijan
```
