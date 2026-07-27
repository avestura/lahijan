#!/usr/bin/env bash
# Lahijan — production install script (WS-23).
#
# Brings up a fresh Lahijan stack on a Linux or macOS host. The script:
#   1. Verifies prerequisites (Docker, Incus, ports 80/443 free).
#   2. Clones or refreshes the Lahijan repo.
#   3. Renders deployments/.env.prod from .env.prod.example, prompting
#      the operator for the required secrets.
#   4. Runs `docker compose up -d` against the prod compose file.
#   5. Waits for the Lahijan healthcheck to flip green.
#   6. Prints the first-run admin credentials from the Lahijan container
#      log (the bootstrap package logs them once at WARN).
#
# Usage:
#   scripts/install.sh [--repo-url URL] [--repo-branch ref] [--install-dir DIR]
#
# Defaults:
#   --repo-url     https://github.com/avestura/lahijan.git
#   --repo-branch  main
#   --install-dir  /opt/lahijan  (Linux) / ~/lahijan (macOS)
#
# Idempotent: re-running the script on an already-installed host re-runs
# `docker compose up -d` (which is itself idempotent) + re-renders .env.prod
# from the operator's answers.
#
# The script is intentionally defensive: every failure mode prints a clear
# message + a hint about how to fix it. Treat the install script as the
# first impression for an external operator — it should feel polished.
set -euo pipefail

# ----------------------------------------------------------------------------
# Constants + defaults
# ----------------------------------------------------------------------------

DEFAULT_REPO_URL="https://github.com/avestura/lahijan.git"
DEFAULT_REPO_BRANCH="main"
DEFAULT_INSTALL_DIR_LINUX="/opt/lahijan"
DEFAULT_INSTALL_DIR_MACOS="$HOME/lahijan"

# Color codes — disabled when stdout is not a TTY so logs are grep-able.
if [ -t 1 ]; then
	C_RESET='\033[0m'
	C_BOLD='\033[1m'
	C_RED='\033[31m'
	C_GREEN='\033[32m'
	C_YELLOW='\033[33m'
	C_BLUE='\033[34m'
else
	C_RESET=""; C_BOLD=""; C_RED=""; C_GREEN=""; C_YELLOW=""; C_BLUE=""
fi

log()  { printf '%b==>%b %s\n' "$C_BLUE" "$C_RESET" "$*"; }
ok()   { printf '%b✓%b %s\n'   "$C_GREEN" "$C_RESET" "$*"; }
warn() { printf '%b!%b %s\n'   "$C_YELLOW" "$C_RESET" "$*" >&2; }
die()  { printf '%b✗%b %s\n'   "$C_RED" "$C_RESET" "$*" >&2; exit 1; }

# ----------------------------------------------------------------------------
# Argument parsing
# ----------------------------------------------------------------------------

REPO_URL="$DEFAULT_REPO_URL"
REPO_BRANCH="$DEFAULT_REPO_BRANCH"
INSTALL_DIR=""

while [ $# -gt 0 ]; do
	case "$1" in
		--repo-url)     REPO_URL="$2"; shift 2 ;;
		--repo-branch)  REPO_BRANCH="$2"; shift 2 ;;
		--install-dir)  INSTALL_DIR="$2"; shift 2 ;;
		-h|--help)
			grep '^#' "$0" | sed 's/^# \{0,1\}//'
			exit 0
			;;
		*) die "unknown argument: $1 (try --help)" ;;
	esac
done

if [ -z "$INSTALL_DIR" ]; then
	case "$(uname -s)" in
		Linux*)  INSTALL_DIR="$DEFAULT_INSTALL_DIR_LINUX" ;;
		Darwin*) INSTALL_DIR="$DEFAULT_INSTALL_DIR_MACOS" ;;
		*) die "unsupported OS: $(uname -s). Use Linux or macOS (or invoke docker compose manually)." ;;
	esac
fi

ENV_FILE="$INSTALL_DIR/deployments/.env.prod"
COMPOSE_FILE="$INSTALL_DIR/deployments/docker-compose.prod.yml"

# ----------------------------------------------------------------------------
# Prerequisites
# ----------------------------------------------------------------------------

log "Checking prerequisites..."

command -v docker >/dev/null 2>&1 || die "docker not found on PATH. Install Docker Engine or Docker Desktop."
docker version >/dev/null 2>&1 || die "docker daemon not running. Start it (sudo systemctl start docker on Linux, open Docker.app on macOS)."

if ! docker compose version >/dev/null 2>&1; then
	die "docker compose subcommand not available. Install Docker Compose v2 (bundled with modern Docker Engine)."
fi

case "$(uname -s)" in
	Linux*)
		# Per ADR-0040 Incus runs containerized in the compose stack
		# (image ghcr.io/cmspam/incus-docker:lts). No host `incus`
		# install is required — Docker Engine alone is enough. We still
		# check for the kernel modules Incus needs (vhost_vsock, kvm,
		# veth, bridge) so a missing module fails fast with a clear
		# message instead of an obscure daemon stack trace.
		for mod in vhost_vsock veth bridge; do
			if [ -d "/sys/module/$mod" ]; then
				ok "kernel module '$mod' loaded"
			else
				warn "kernel module '$mod' not loaded."
				warn "  The containerized Incus can usually modprobe it from /lib/modules."
				warn "  If the daemon fails to start with 'socket: function not implemented',"
				warn "  run: sudo modprobe $mod   (and add to /etc/modules-load.d/ for persistence)."
			fi
		done
		# Ensure the host path /var/lib/incus exists so the prod compose
		# bind-mount does not silently create an empty dir owned by root.
		if [ ! -d /var/lib/incus ]; then
			warn "/var/lib/incus does not exist; the prod compose will create it as root-owned."
			warn "If you have an existing host-installed Incus, point the bind-mount there."
		else
			ok "/var/lib/incus exists (Incus state dir)"
		fi
		;;
	*)
		warn "$(uname -s) host: containerized Incus may fail to start"
		warn "(Docker Desktop's VM may block AF_VSOCK for privileged containers)."
		warn "If the incus service fails with 'socket: function not implemented',"
		warn "follow the WSL2 fallback in docs/howto/run-incus-on-windows.md (Windows)"
		warn "or deploy on a Linux VM for full functionality."
		;;
esac

# Disk space — Postgres + SeaweedFS + images need a comfortable floor.
FREE_GB=$(df -BG "$INSTALL_DIR" 2>/dev/null | awk 'NR==2 {gsub("G","",$4); print $4}' || echo 0)
if [ "$FREE_GB" -lt 10 ]; then
	warn "less than 10 GB free on $INSTALL_DIR (have ${FREE_GB}G). Lahijan needs headroom for Postgres + SeaweedFS volumes."
fi

# Ports 80 + 443 — Caddy needs them. Anything else listening is a hard fail.
for port in 80 443; do
	if command -v ss >/dev/null 2>&1; then
		if ss -ltn "( sport = :$port )" 2>/dev/null | grep -q LISTEN; then
			die "port $port is in use. Free it (or stop the service) before re-running install."
		fi
	fi
done

ok "prerequisites met."

# ----------------------------------------------------------------------------
# Clone / refresh the repo
# ----------------------------------------------------------------------------

if [ -d "$INSTALL_DIR/.git" ]; then
	log "Updating existing install at $INSTALL_DIR..."
	git -C "$INSTALL_DIR" fetch --quiet origin "$REPO_BRANCH" \
		|| die "git fetch failed (network or auth)."
	git -C "$INSTALL_DIR" checkout --quiet "$REPO_BRANCH" \
		|| die "git checkout failed (local changes?)."
	git -C "$INSTALL_DIR" reset --quiet --hard "origin/$REPO_BRANCH" \
		|| die "git reset failed."
else
	log "Cloning Lahijan into $INSTALL_DIR..."
	if [ -e "$INSTALL_DIR" ] && [ ! -d "$INSTALL_DIR" ]; then
		die "$INSTALL_DIR exists but is not a directory. Move it aside."
	fi
	mkdir -p "$(dirname "$INSTALL_DIR")"
	git clone --depth=1 --branch "$REPO_BRANCH" "$REPO_URL" "$INSTALL_DIR" \
		|| die "git clone failed (network, URL, or auth)."
fi
ok "code at $(git -C "$INSTALL_DIR" log -1 --format='%h %s')"

# ----------------------------------------------------------------------------
# Render .env.prod
# ----------------------------------------------------------------------------

if [ ! -f "$ENV_FILE" ]; then
	log "Rendering $ENV_FILE from template..."
	cp "$INSTALL_DIR/deployments/.env.prod.example" "$ENV_FILE"

	prompt() {
		# prompt <var-name> <message> <default>
		local var="$1" msg="$2" def="${3-}"
		local val
		if [ -n "$def" ]; then
			read -r -p "  $msg [$def]: " val || true
			val="${val:-$def}"
		else
			read -r -p "  $msg: " val || true
		fi
		# Escape any forward slashes + ampersands for sed's s/// replacement.
		val_escaped=$(printf '%s' "$val" | sed -e 's/[\/&]/\\&/g')
		# Replace the first occurrence of the placeholder line.
		sed -i.bak -E "s|^${var}=.*|${var}=${val_escaped}|" "$ENV_FILE"
		rm -f "$ENV_FILE.bak"
	}

	cat <<EOF

${C_BOLD}Lahijan first-time setup.${C_RESET}
Answer the prompts; values are written to $ENV_FILE.
EOF

	prompt LAHIJAN_PUBLIC_HOST        "Public hostname (must resolve to this host)" "app.example.com"
	prompt LAHIJAN_PUBLIC_URL         "Public URL (scheme + host)"                  "https://app.example.com"
	prompt LAHIJAN_ACME_EMAIL         "Email for Let's Encrypt expiry notices"
	prompt LAHIJAN_IMAGE_TAG          "Lahijan image tag (SemVer)"                  "v0.1.0"
	prompt LAHIJAN_BOOTSTRAP_ADMIN_EMAIL "First-run platform.admin email"          "admin@example.com"
	prompt POSTGRES_SUPERUSER_PASSWORD "Postgres superuser password (32+ chars)"
	prompt LAHIJAN_DATABASE_PASSWORD   "Lahijan DB password (32+ chars)"
	prompt PDNS_DATABASE_PASSWORD     "PowerDNS DB password (32+ chars)"
	prompt SEAWEED_DATABASE_PASSWORD  "SeaweedFS DB password (32+ chars)"
	prompt PDNS_API_KEY               "PowerDNS API key (32 hex chars)"
	prompt SEAWEEDFS_S3_ACCESS_KEY    "SeaweedFS admin access key (16 hex chars)"
	prompt SEAWEEDFS_S3_SECRET_KEY    "SeaweedFS admin secret key (32 hex chars)"
	prompt LAHIJAN_AUTH_SIGNING_KEY   "Auth HMAC signing key (48+ chars)"
	prompt LAHIJAN_AUTH_SECRETS_ENCRYPTIONKEY "Auth AES-256-GCM key (base64 32 bytes)"
	prompt LAHIJAN_SMTP_HOST          "SMTP host"
	prompt LAHIJAN_SMTP_USER          "SMTP user"
	prompt LAHIJAN_SMTP_PASSWORD      "SMTP password"
	prompt GRAFANA_ADMIN_PASSWORD     "Grafana admin password"

	ok "$ENV_FILE rendered."
else
	ok "$ENV_FILE already exists; leaving values in place."
fi

# ----------------------------------------------------------------------------
# Bring up the stack
# ----------------------------------------------------------------------------

log "Pulling images..."
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" pull --quiet

log "Bringing up the stack (this can take a few minutes on first boot)..."
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" up -d

# ----------------------------------------------------------------------------
# Wait for the Lahijan healthcheck to flip green
# ----------------------------------------------------------------------------

log "Waiting for Lahijan to become healthy..."
attempt=0
max_attempts=60
until docker inspect --format='{{json .State.Health.Status}}' lahijan-prod-app 2>/dev/null | grep -q '"healthy"'; do
	attempt=$((attempt + 1))
	if [ "$attempt" -ge "$max_attempts" ]; then
		warn "Lahijan did not become healthy within $((max_attempts * 5)) seconds."
		warn "Inspect logs with: docker compose --env-file $ENV_FILE -f $COMPOSE_FILE logs lahijan"
		exit 1
	fi
	printf '.'
	sleep 5
done
echo
ok "Lahijan is healthy."

# ----------------------------------------------------------------------------
# Print first-run admin credentials (WS-23 bootstrap).
# ----------------------------------------------------------------------------

log "Looking for first-run admin credentials in the Lahijan log..."
sleep 2 # give slog a beat to flush
creds_line=$(docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" logs lahijan 2>/dev/null \
	| grep 'first-run admin credentials' | tail -n 1 || true)

if [ -n "$creds_line" ]; then
	cat <<EOF

${C_BOLD}${C_GREEN}Lahijan is live.${C_RESET}

${C_BOLD}First-run admin credentials (rotate immediately):${C_RESET}
$creds_line

Dashboard: ${LAHIJAN_PUBLIC_URL:-https://app.example.com}
Grafana:   ${LAHIJAN_PUBLIC_URL:-https://app.example.com}/grafana

${C_YELLOW}WARNING:${C_RESET} the password above is shown only once. Rotate it via the
dashboard after your first login. See deployments/SECRETS.md for the
rotation procedure.

Next: open the dashboard URL, log in with the admin email + password,
and complete the post-install checklist in deployments/README.md.
EOF
else
	cat <<EOF

${C_BOLD}${C_GREEN}Lahijan is live.${C_RESET}

The first-run admin credentials line was not found in the Lahijan log.
This is expected when the DB was not empty on this boot (e.g. on upgrade
or after a `docker compose down` + `up` cycle).

If this is a fresh install, check the bootstrap log:
  docker compose --env-file $ENV_FILE -f $COMPOSE_FILE logs lahijan | grep bootstrap

Dashboard: ${LAHIJAN_PUBLIC_URL:-https://app.example.com}
EOF
fi

ok "install.sh complete."
