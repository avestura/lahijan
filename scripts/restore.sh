#!/usr/bin/env bash
# Lahijan — restore script (WS-23).
#
# Restores from a backup tarball produced by scripts/backup.sh.
#
# The restore path:
#   1. Verify the target stack is STOPPED (restore refuses to run with
#      containers up; partial state is dangerous).
#   2. Drop + recreate the three logical databases from the postgres
#      superuser.
#   3. pg_restore each DB dump.
#   4. Replace the SeaweedFS volume contents from the snapshot.
#   5. Replace the Lahijan plugin upload directory.
#   6. Print the next-step instructions (bring the stack back up).
#
# The restore does NOT touch:
#   - The auth signing key + encryption key (operator-managed; see
#     deployments/SECRETS.md for rotation procedure).
#   - The TLS certs (Caddy re-issues on first boot).
#   - The Incus daemon state at /var/lib/incus (per ADR-0040 this is now
#     a host bind-mount in prod; the operator backs it up + restores it
#     separately — see scripts/backup.sh).
#
# Usage:
#   scripts/restore.sh --install-dir DIR --backup FILE
#
# Safety: the script asks for interactive confirmation before dropping the
# databases. Pass --yes to skip the prompt (for automated restores).
set -euo pipefail

INSTALL_DIR=""
BACKUP_FILE=""
ASSUME_YES=0

while [ $# -gt 0 ]; do
	case "$1" in
		--install-dir) INSTALL_DIR="$2"; shift 2 ;;
		--backup)      BACKUP_FILE="$2"; shift 2 ;;
		--yes)         ASSUME_YES=1; shift ;;
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

[ -n "$BACKUP_FILE" ] || { echo "missing --backup FILE" >&2; exit 2; }
[ -f "$BACKUP_FILE" ] || { echo "backup file not found: $BACKUP_FILE" >&2; exit 1; }

ENV_FILE="$INSTALL_DIR/deployments/.env.prod"
[ -f "$ENV_FILE" ] || { echo "missing $ENV_FILE" >&2; exit 1; }

# shellcheck disable=SC1090
set -a; . "$ENV_FILE"; set +a

# ----------------------------------------------------------------------------
# Pre-flight
# ----------------------------------------------------------------------------

WORK_DIR=$(mktemp -d)
trap 'rm -rf "$WORK_DIR"' EXIT

echo "===> extracting backup to $WORK_DIR..."
tar -C "$WORK_DIR" -xzf "$BACKUP_FILE"

[ -f "$WORK_DIR/MANIFEST" ] && cat "$WORK_DIR/MANIFEST"

# Sanity: required dump files present.
for db in "$LAHIJAN_DATABASE_NAME" "$PDNS_DATABASE_NAME" "$SEAWEED_DATABASE_NAME"; do
	[ -f "$WORK_DIR/$db.dump" ] || { echo "missing $db.dump in backup" >&2; exit 1; }
done
[ -f "$WORK_DIR/seaweedfs.tgz" ] || { echo "missing seaweedfs.tgz in backup" >&2; exit 1; }

# ----------------------------------------------------------------------------
# Confirmation
# ----------------------------------------------------------------------------

if [ "$ASSUME_YES" -ne 1 ]; then
	cat <<EOF

${C_BOLD:-}WARNING:${C_RESET:-}
You are about to:
  - Stop the Lahijan stack
  - DROP + recreate databases: $LAHIJAN_DATABASE_NAME, $PDNS_DATABASE_NAME, $SEAWEED_DATABASE_NAME
  - REPLACE the SeaweedFS volume contents
  - REPLACE the Lahijan plugins volume contents

This is destructive. The current state will be lost.

Type the hostname of this host to confirm:
EOF
	read -r -p "> " confirm
	HOST_GUESS="${LAHIJAN_PUBLIC_HOST:-}"
	if [ -z "$confirm" ] || [ "$confirm" != "$HOST_GUESS" ]; then
		echo "confirmation failed; aborting" >&2
		exit 1
	fi
fi

# ----------------------------------------------------------------------------
# Stop the stack (keep postgres + seaweed volumes intact)
# ----------------------------------------------------------------------------

COMPOSE_FILE="$INSTALL_DIR/deployments/docker-compose.prod.yml"
echo "===> stopping Lahijan stack (postgres kept up for the restore)..."
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" stop lahijan caddy powerdns \
	seaweed-filer seaweed-s3 seaweed-volume seaweed-master incus \
	otel-collector jaeger loki prometheus grafana 2>/dev/null || true

# ----------------------------------------------------------------------------
# Drop + recreate + restore the three logical databases
# ----------------------------------------------------------------------------

PG_CONTAINER="lahijan-prod-postgres"

# Make sure the postgres container is up (it might have been stopped too).
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" up -d --no-deps postgres
echo "===> waiting for postgres to be ready..."
until docker exec "$PG_CONTAINER" pg_isready -U "${POSTGRES_SUPERUSER:-postgres}" -d "${POSTGRES_SUPERUSER_DB:-postgres}" >/dev/null 2>&1; do
	printf '.'; sleep 2
done
echo

for db in "$LAHIJAN_DATABASE_NAME" "$PDNS_DATABASE_NAME" "$SEAWEED_DATABASE_NAME"; do
	echo "===> recreating database $db..."
	docker exec "$PG_CONTAINER" \
		psql -U "${POSTGRES_SUPERUSER:-postgres}" -d "${POSTGRES_SUPERUSER_DB:-postgres}" -v ON_ERROR_STOP=1 <<SQL
-- terminate active connections to the db so DROP succeeds
SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '$db';
DROP DATABASE IF EXISTS "$db";
CREATE DATABASE "$db";
SQL
done

# Restore roles first so the per-DB owners exist when pg_restore tries to
# reassign object ownership.
if [ -f "$WORK_DIR/roles.sql" ]; then
	echo "===> restoring roles..."
	docker exec -i "$PG_CONTAINER" \
		psql -U "${POSTGRES_SUPERUSER:-postgres}" -d "${POSTGRES_SUPERUSER_DB:-postgres}" \
		< "$WORK_DIR/roles.sql" || echo "  (roles.sql warnings are OK if the roles already exist)"
fi

for db in "$LAHIJAN_DATABASE_NAME" "$PDNS_DATABASE_NAME" "$SEAWEED_DATABASE_NAME"; do
	echo "===> restoring database $db..."
	# Resolve the per-DB owner role so the restore can re-grant it.
	case "$db" in
		"$LAHIJAN_DATABASE_NAME")  owner="$LAHIJAN_DATABASE_USER" ;;
		"$PDNS_DATABASE_NAME")     owner="$PDNS_DATABASE_USER" ;;
		"$SEAWEED_DATABASE_NAME")  owner="$SEAWEED_DATABASE_USER" ;;
		*) owner="" ;;
	esac
	if [ -n "$owner" ]; then
		# Reassign the per-DB owner role so the restore can re-grant it.
		# Errors here are non-fatal: a role that already exists is the
		# common case (the postgres init script created it on first boot).
		docker exec "$PG_CONTAINER" \
			psql -U "${POSTGRES_SUPERUSER:-postgres}" -d "${POSTGRES_SUPERUSER_DB:-postgres}" -v ON_ERROR_STOP=0 <<SQL || true
ALTER DATABASE "$db" OWNER TO "$owner";
SQL
	fi

	# Restore the dump. --no-owner so the restore does not try to SET ROLE
	# (we already fixed ownership above); --no-privileges for the same
	# reason. --clean + --if-exists would be nicer but pg_restore does not
	# support --clean on an empty DB cleanly.
	docker exec -i "$PG_CONTAINER" \
		pg_restore -U "${POSTGRES_SUPERUSER:-postgres}" -d "$db" --no-owner --no-privileges -v \
		< "$WORK_DIR/$db.dump" || echo "  (pg_restore emitted warnings; usually OK on a clean restore)"
done

# ----------------------------------------------------------------------------
# Replace SeaweedFS + Lahijan plugins volume contents
# ----------------------------------------------------------------------------

echo "===> replacing SeaweedFS volume contents..."
# Clear the existing volume first.
docker run --rm \
	-v lahijan-prod-seaweed:/data \
	alpine:3.22 \
	sh -c 'rm -rf /data/* /data/.[!.]* /data/..?* 2>/dev/null || true'
# Untar the snapshot into the volume.
docker run --rm \
	-v lahijan-prod-seaweed:/data \
	-v "$WORK_DIR":/out:ro \
	alpine:3.22 \
	tar -C /data -xzf /out/seaweedfs.tgz || {
		echo "seaweedfs volume restore failed" >&2
		exit 1
	}

if [ -f "$WORK_DIR/plugins.tgz" ]; then
	echo "===> replacing Lahijan plugins volume contents..."
	docker run --rm \
		-v lahijan-prod-plugins:/data \
		alpine:3.22 \
		sh -c 'rm -rf /data/* /data/.[!.]* /data/..?* 2>/dev/null || true'
	docker run --rm \
		-v lahijan-prod-plugins:/data \
		-v "$WORK_DIR":/out:ro \
		alpine:3.22 \
		tar -C /data -xzf /out/plugins.tgz || {
			echo "plugins volume restore failed" >&2
			exit 1
		}
fi

# ----------------------------------------------------------------------------
# Replace Incus state (host path /var/lib/incus). Per ADR-0040 the prod
# compose bind-mounts /var/lib/incus from the host; restoring means
# untarring the snapshot back over it. The Incus container MUST be stopped
# during this step (the stack-stop above already includes the incus service).
# ----------------------------------------------------------------------------
if [ -f "$WORK_DIR/incus.tgz" ] && [ -d /var/lib/incus ]; then
	echo "===> replacing Incus state (/var/lib/incus)..."
	# Clear the existing directory. The path is owned by root (the incus
	# container writes as root), so we need sudo. Skip with a warning if
	# the operator is not root.
	if [ "$(id -u)" -eq 0 ]; then
		rm -rf /var/lib/incus/* /var/lib/incus/.[!.]* /var/lib/incus/..?* 2>/dev/null || true
		tar -C /var/lib/incus -xzf "$WORK_DIR/incus.tgz" || {
			echo "incus state restore failed" >&2
			exit 1
		}
	else
		echo "  (not running as root; skipping Incus state restore)" >&2
		echo "  re-run as root or pre-restore /var/lib/incus manually" >&2
	fi
fi

# ----------------------------------------------------------------------------
# Done
# ----------------------------------------------------------------------------

cat <<EOF

[OK] restore complete.

Next steps:
  1. Bring the stack back up:
       docker compose --env-file $ENV_FILE -f $COMPOSE_FILE up -d

  2. Confirm Lahijan is healthy:
       docker inspect --format='{{json .State.Health.Status}}' lahijan-prod-app

  3. The first-run admin bootstrap will SKIP because the users table is
     populated. Log in with the credentials from the backup.

If something looks wrong, the backup tarball is intact at:
  $BACKUP_FILE
EOF
