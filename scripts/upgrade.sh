#!/usr/bin/env bash
# Lahijan — production upgrade script (WS-23).
#
# Pulls the latest Lahijan image + git ref, applies DB migrations, and
# restarts services in dependency order. Idempotent.
#
# The restart order matters:
#   1. postgres        (storage layer first; nothing else starts without it)
#   2. powerdns        (depends on postgres)
#   3. seaweed-master  → seaweed-volume → seaweed-filer → seaweed-s3
#   4. incus-client    (no DB dependency; can run in parallel)
#   5. otel-collector  (so Lahijan's first request is observed)
#   6. lahijan         (the only service that runs DB migrations)
#   7. caddy           (last; flip the proxy after the backend is ready)
#
# Migrations are run by the Lahijan container itself (its bootstrap runs
# golang-migrate's `up` command before starting Fiber). The script does
# NOT apply migrations out-of-band; it just ensures the Lahijan container
# is the only one restarted during a version bump.
#
# Usage:
#   scripts/upgrade.sh [--install-dir DIR] [--tag TAG]
#
# Defaults:
#   --install-dir  /opt/lahijan  (Linux) / ~/lahijan (macOS)
#   --tag          reads LAHIJAN_IMAGE_TAG from .env.prod
#
# Safety: the script refuses to upgrade if the Lahijan healthcheck is
# currently failing (an upgrade on a broken stack just makes debugging
# harder). Override with --force.
set -euo pipefail

INSTALL_DIR=""
TAG=""
FORCE=0

while [ $# -gt 0 ]; do
	case "$1" in
		--install-dir) INSTALL_DIR="$2"; shift 2 ;;
		--tag)         TAG="$2"; shift 2 ;;
		--force)       FORCE=1; shift ;;
		-h|--help)
			grep '^#' "$0" | sed 's/^# \{0,1\}//'
			exit 0
			;;
		*) echo "unknown argument: $1" >&2; exit 2 ;;
	esac
done

if [ -z "$INSTALL_DIR" ]; then
	case "$(uname -s)" in
		Linux*)  INSTALL_DIR="/opt/lahijan" ;;
		Darwin*) INSTALL_DIR="$HOME/lahijan" ;;
		*) echo "unsupported OS: $(uname -s)" >&2; exit 2 ;;
	esac
fi

ENV_FILE="$INSTALL_DIR/deployments/.env.prod"
COMPOSE_FILE="$INSTALL_DIR/deployments/docker-compose.prod.yml"

[ -f "$ENV_FILE" ]    || { echo "missing $ENV_FILE — run install.sh first" >&2; exit 1; }
[ -f "$COMPOSE_FILE" ] || { echo "missing $COMPOSE_FILE — wrong install dir?" >&2; exit 1; }

# shellcheck disable=SC1090
set -a; . "$ENV_FILE"; set +a

# ---- Pre-flight: current stack must be healthy ----------------------------

CURRENT_TAG="${TAG:-${LAHIJAN_IMAGE_TAG:-unknown}}"
echo "===> upgrading Lahijan to tag: $CURRENT_TAG"

if [ "$FORCE" -ne 1 ]; then
	health=$(docker inspect --format='{{json .State.Health.Status}}' lahijan-prod-app 2>/dev/null || echo '"missing"')
	if [ "$health" != '"healthy"' ]; then
		echo "Lahijan is not healthy (status: $health). Refusing to upgrade." >&2
		echo "Run 'docker compose ... logs lahijan' to investigate, then re-run with --force." >&2
		exit 1
	fi
fi

# ---- Pull the new code + image --------------------------------------------

echo "===> pulling latest code..."
git -C "$INSTALL_DIR" fetch --quiet origin
git -C "$INSTALL_DIR" reset --quiet --hard "origin/${LAHIJAN_GIT_BRANCH:-main}"

echo "===> pulling images..."
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" pull --quiet lahijan

# ---- Restart services in dependency order ---------------------------------
#
# Postgres + PDNS + SeaweedFS do not need a restart for a Lahijan-only
# version bump (their config is stable). We DO restart OTel + Lahijan +
# Caddy to ensure the new Lahijan binary is wired up + the collector
# reconnects.

echo "===> restarting otel-collector..."
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" up -d --no-deps otel-collector

echo "===> restarting lahijan (runs migrations on boot)..."
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" up -d --no-deps --force-recreate lahijan

# Wait for Lahijan to flip healthy before flipping the proxy. The
# healthcheck is the source of truth — it covers "migrations finished +
# Fiber listening + DB pool live".
echo "===> waiting for Lahijan healthcheck..."
attempt=0
max_attempts=60
until docker inspect --format='{{json .State.Health.Status}}' lahijan-prod-app 2>/dev/null | grep -q '"healthy"'; do
	attempt=$((attempt + 1))
	if [ "$attempt" -ge "$max_attempts" ]; then
		echo "Lahijan did not become healthy after upgrade." >&2
		echo "Inspect logs with: docker compose --env-file $ENV_FILE -f $COMPOSE_FILE logs lahijan" >&2
		exit 1
	fi
	printf '.'
	sleep 5
done
echo

echo "===> restarting caddy (flip the proxy)..."
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" up -d --no-deps --force-recreate caddy

# ---- Done -----------------------------------------------------------------

echo
echo "[OK] upgrade complete."
echo "    version: $(docker inspect --format='{{.Config.Image}}' lahijan-prod-app 2>/dev/null || echo '?')"
echo "    health:  $(docker inspect --format='{{json .State.Health.Status}}' lahijan-prod-app 2>/dev/null || echo '?')"
echo
echo "Rollback: set LAHIJAN_IMAGE_TAG=<old-tag> in $ENV_FILE and re-run this script."
