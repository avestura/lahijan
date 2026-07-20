#!/usr/bin/env bash
# Lahijan — bare-metal systemd installer.
#
# Copies the unit file to /lib/systemd/system, creates the lahijan system
# user + directories, and enables + starts the service. Idempotent.
#
# This is NOT the recommended install path for most operators — use
# scripts/install.sh (docker compose) instead. See init/README.md.
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
	echo "must be run as root (or via sudo)" >&2
	exit 1
fi

UNIT_SRC="$(dirname "$0")/lahijan.service"
UNIT_DST="/lib/systemd/system/lahijan.service"
BIN_SRC="/usr/local/bin/lahijan"
CONF_DIR="/etc/lahijan"
DATA_DIR="/var/lib/lahijan"

[ -f "$UNIT_SRC" ] || { echo "missing $UNIT_SRC" >&2; exit 1; }
[ -x "$BIN_SRC" ]  || {
	echo "missing executable $BIN_SRC" >&2
	echo "  build + install first: make build && sudo cp dist/lahijan $BIN_SRC" >&2
	exit 1
}

echo "===> creating lahijan system user + directories..."
if ! id lahijan >/dev/null 2>&1; then
	useradd --system --no-create-home --shell /usr/sbin/nologin lahijan
fi
mkdir -p "$CONF_DIR" "$DATA_DIR"
chown -R lahijan:lahijan "$DATA_DIR"

echo "===> installing unit file..."
install -m 0644 "$UNIT_SRC" "$UNIT_DST"
systemctl daemon-reload

echo "===> enabling + starting lahijan..."
systemctl enable lahijan
systemctl restart lahijan

echo "[OK] lahijan is installed + running."
echo "    logs:      journalctl -u lahijan -f"
echo "    config:    $CONF_DIR/lahijan.env (create this with your secrets)"
echo "    data:      $DATA_DIR"
