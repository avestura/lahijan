#!/usr/bin/env bash
# Lahijan — backup script (WS-23).
#
# Produces a single tarball containing:
#   - pg_dump of the three logical databases (lahijan, pdns, seaweed) in
#     custom format (parallel-restore-friendly, compressed)
#   - the SeaweedFS volume's data directory (snapshot via `docker run
#     --rm -v <volume>:/data tar` so we do not need SSH into the container)
#   - the Lahijan plugin upload directory (small; lives on a named volume)
#   - .env.prod minus the secrets we can regenerate (signing key, encryption
#     key are NOT dumped — those are operator-managed per deployments/SECRETS.md)
#
# The tarball is written to LAHIJAN_BACKUP_LOCAL_DIR (default ./backups).
# When LAHIJAN_BACKUP_S3_BUCKET is non-empty, the tarball is additionally
# pushed to the configured S3-compatible endpoint via aws-cli.
#
# Backups older than LAHIJAN_BACKUP_RETENTION_DAYS are pruned at the end
# of every run (local + S3).
#
# Usage:
#   scripts/backup.sh [--install-dir DIR] [--out FILE]
#
# Cron example (02:00 daily, root):
#   0 2 * * * /opt/lahijan/scripts/backup.sh --install-dir /opt/lahijan >> /var/log/lahijan-backup.log 2>&1
set -euo pipefail

INSTALL_DIR=""
OUT_FILE=""

while [ $# -gt 0 ]; do
	case "$1" in
		--install-dir) INSTALL_DIR="$2"; shift 2 ;;
		--out)         OUT_FILE="$2"; shift 2 ;;
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
[ -f "$ENV_FILE" ] || { echo "missing $ENV_FILE" >&2; exit 1; }

# shellcheck disable=SC1090
set -a; . "$ENV_FILE"; set +a

: "${LAHIJAN_BACKUP_LOCAL_DIR:=./backups}"
: "${LAHIJAN_BACKUP_RETENTION_DAYS:=14}"
mkdir -p "$LAHIJAN_BACKUP_LOCAL_DIR"

STAMP=$(date -u +%Y%m%dT%H%M%SZ)
if [ -z "$OUT_FILE" ]; then
	OUT_FILE="$LAHIJAN_BACKUP_LOCAL_DIR/lahijan-backup-$STAMP.tar.gz"
fi
WORK_DIR=$(mktemp -d)
trap 'rm -rf "$WORK_DIR"' EXIT

echo "===> starting Lahijan backup -> $OUT_FILE"

# ----------------------------------------------------------------------------
# Postgres: pg_dump all three logical DBs in custom format.
#
# We use the postgres container's psql + pg_dump via `docker exec` so we do
# not need pg tools on the operator host. The custom format (-Fc) gives
# us parallel restore + compression; the compression level is left at
# default (6) which is fast + small enough for typical Lahijan data sizes.
# ----------------------------------------------------------------------------

echo "  dumping Postgres databases..."
PG_CONTAINER="lahijan-prod-postgres"
for db in "$LAHIJAN_DATABASE_NAME" "$PDNS_DATABASE_NAME" "$SEAWEED_DATABASE_NAME"; do
	echo "    - $db"
	docker exec "$PG_CONTAINER" \
		pg_dump -U "${POSTGRES_SUPERUSER:-postgres}" -d "$db" -Fc --no-owner --no-privileges \
		> "$WORK_DIR/$db.dump" || {
			echo "pg_dump of $db failed" >&2
			exit 1
		}
done

# Also dump the global objects (roles + tablespaces) so a bare-metal
# restore can recreate the schema_migrations + River schema owners.
docker exec "$PG_CONTAINER" \
	pg_dumpall -U "${POSTGRES_SUPERUSER:-postgres}" --roles-only \
	> "$WORK_DIR/roles.sql" || {
		echo "pg_dumpall (roles) failed" >&2
		exit 1
	}

# ----------------------------------------------------------------------------
# SeaweedFS: snapshot the volume by tar-ing it from a one-shot alpine
# container that mounts the named volume. This is the documented SeaweedFS
# backup pattern; the volume contains only blob data (metadata is in
# Postgres).
# ----------------------------------------------------------------------------

echo "  snapshotting SeaweedFS volume..."
docker run --rm \
	-v lahijan-prod-seaweed:/data:ro \
	-v "$WORK_DIR":/out \
	alpine:3.22 \
	tar -C /data -czf /out/seaweedfs.tgz . || {
		echo "seaweedfs volume tar failed" >&2
		exit 1
	}

# ----------------------------------------------------------------------------
# Lahijan plugins: small named volume; same pattern.
# ----------------------------------------------------------------------------

echo "  snapshotting Lahijan plugins volume..."
docker run --rm \
	-v lahijan-prod-plugins:/data:ro \
	-v "$WORK_DIR":/out \
	alpine:3.22 \
	tar -C /data -czf /out/plugins.tgz . || {
		echo "plugins volume tar failed" >&2
		exit 1
	}

# ----------------------------------------------------------------------------
# Incus: snapshot the host bind-mounted /var/lib/incus directory (ADR-0040).
# Per ADR-0040 the Incus state lives at /var/lib/incus on the HOST (the
# compose stack bind-mounts it into the incus container). We tar the host
# path directly (no docker run needed) so the snapshot includes everything
# the daemon has written: database, images, instances, snapshots.
#
# Skip silently if /var/lib/incus does not exist (e.g. on a stack that uses
# the alternative named-volume topology or where Incus is disabled).
# ----------------------------------------------------------------------------
if [ -d /var/lib/incus ]; then
	echo "  snapshotting Incus state (/var/lib/incus)..."
	tar -C /var/lib/incus -czf "$WORK_DIR/incus.tgz" . 2>/dev/null || {
		echo "incus state tar failed (continuing; Incus may be running)" >&2
		rm -f "$WORK_DIR/incus.tgz"
	}
else
	echo "  skipping Incus state (/var/lib/incus not present on host)"
fi

# ----------------------------------------------------------------------------
# Manifest: env vars we need to restore (NON-secret ones).
# ----------------------------------------------------------------------------

cat > "$WORK_DIR/MANIFEST" <<EOF
lahijan_backup_stamp=$STAMP
lahijan_image_tag=${LAHIJAN_IMAGE_TAG:-unknown}
postgres_image=$(docker inspect --format='{{.Config.Image}}' lahijan-prod-postgres 2>/dev/null || echo unknown)
seaweed_image=$(docker inspect --format='{{.Config.Image}}' lahijan-prod-seaweed-master 2>/dev/null || echo unknown)
pdns_image=$(docker inspect --format='{{.Config.Image}}' lahijan-prod-powerdns 2>/dev/null || echo unknown)
postgres_db_names=$LAHIJAN_DATABASE_NAME,$PDNS_DATABASE_NAME,$SEAWEED_DATABASE_NAME
EOF

# ----------------------------------------------------------------------------
# Bundle
# ----------------------------------------------------------------------------

echo "  bundling..."
tar -C "$WORK_DIR" -czf "$OUT_FILE" . || {
	echo "tar failed" >&2
	exit 1
}

SIZE=$(du -h "$OUT_FILE" | awk '{print $1}')
echo "[OK] local backup written: $OUT_FILE ($SIZE)"

# ----------------------------------------------------------------------------
# S3 upload (when configured)
# ----------------------------------------------------------------------------

if [ -n "${LAHIJAN_BACKUP_S3_BUCKET:-}" ]; then
	if ! command -v aws >/dev/null 2>&1; then
		echo "  aws-cli not found; skipping S3 upload" >&2
	else
		S3_PREFIX="${LAHIJAN_BACKUP_S3_PREFIX:-lahijan/}"
		S3_URI="s3://$LAHIJAN_BACKUP_S3_BUCKET/${S3_PREFIX}$(basename "$OUT_FILE")"
		echo "  uploading to $S3_URI..."
		ENDPOINT_FLAGS=()
		if [ -n "${LAHIJAN_BACKUP_S3_ENDPOINT:-}" ]; then
			ENDPOINT_FLAGS=(--endpoint-url "$LAHIJAN_BACKUP_S3_ENDPOINT")
		fi
		aws "${ENDPOINT_FLAGS[@]}" s3 cp "$OUT_FILE" "$S3_URI" \
			|| { echo "S3 upload failed" >&2; exit 1; }

		# Prune old S3 backups. The aws-cli s3api list-objects-v2 + delete
		# pattern keeps the bucket tidy without a lifecycle policy.
		cutoff=$(date -u -d "-$LAHIJAN_BACKUP_RETENTION_DAYS days" +%Y-%m-%dT%H:%M:%S 2>/dev/null \
			|| date -u -v-${LAHIJAN_BACKUP_RETENTION_DAYS}d +%Y-%m-%dT%H:%M:%S)
		echo "  pruning S3 backups older than $cutoff..."
		# We rely on the backup filename's stamp prefix (YYYYmmddTHHMMSSZ)
		# so the prune is lexical. Anything lexically less than the cutoff
		# stamp is deleted.
		cutoff_filename="lahijan-backup-$(date -u -d "-$LAHIJAN_BACKUP_RETENTION_DAYS days" +%Y%m%dT%H%M%SZ 2>/dev/null \
			|| date -u -v-${LAHIJAN_BACKUP_RETENTION_DAYS}d +%Y%m%dT%H%M%SZ).tar.gz"
		aws "${ENDPOINT_FLAGS[@]}" s3api list-objects-v2 \
			--bucket "$LAHIJAN_BACKUP_S3_BUCKET" \
			--prefix "$S3_PREFIX" \
			--query "Contents[?Key < \`${S3_PREFIX}${cutoff_filename}\`].Key" \
			--output text | xargs -r -n1 -I{} \
			aws "${ENDPOINT_FLAGS[@]}" s3api delete-object \
				--bucket "$LAHIJAN_BACKUP_S3_BUCKET" --key {} 2>/dev/null || true
		echo "[OK] S3 backup uploaded + pruned."
	fi
fi

# ----------------------------------------------------------------------------
# Local retention prune
# ----------------------------------------------------------------------------

echo "  pruning local backups older than $LAHIJAN_BACKUP_RETENTION_DAYS days..."
find "$LAHIJAN_BACKUP_LOCAL_DIR" -name 'lahijan-backup-*.tar.gz' -type f \
	-mtime +"$LAHIJAN_BACKUP_RETENTION_DAYS" -delete 2>/dev/null || true

echo "[OK] backup complete."
