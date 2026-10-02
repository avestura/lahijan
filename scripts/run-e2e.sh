#!/usr/bin/env bash
# scripts/run-e2e.sh — WS-22 e2e harness runner.
#
# Brings up the test sandbox (Postgres + SeaweedFS), starts the in-process
# Incus + PowerDNS fakes, builds + runs the Lahijan app + the dashboard,
# runs Playwright against the dashboard, then tears everything down.
#
# Always tears down on exit (including on failure) so a CI run never
# leaves containers or processes behind. The script is the single source
# of truth for what `make test-e2e` does.
#
# Usage:
#   scripts/run-e2e.sh                 # headless (CI default)
#   LAHIJAN_E2E_HEADED=1 scripts/run-e2e.sh   # show the browser
#   LAHIJAN_E2E_KEEP_UP=1 scripts/run-e2e.sh  # skip teardown (debug only)
#
# Environment overrides:
#   LAHIJAN_E2E_APP_PORT      default 8080
#   LAHIJAN_E2E_DASHBOARD_PORT default 4173
#   LAHIJAN_E2E_INCUS_FAKE_PORT default 18091
#   LAHIJAN_E2E_POWERDNS_FAKE_PORT default 18092
#   LAHIJAN_E2E_POSTGRES_PORT default 55432
#   LAHIJAN_E2E_S3_PORT       default 8334
#   LAHIJAN_E2E_FILER_PORT    default 8889
set -euo pipefail

# ---- Setup -----------------------------------------------------------
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# Run-timeouts (seconds). The WS-22 DoD budget is <15 minutes wall.
WAIT_TIMEOUT=120   # how long to wait for any single service to become healthy
PLAYWRIGHT_TIMEOUT=600  # 10 minutes for the full Playwright suite

# Port configuration (single source of truth).
APP_PORT="${LAHIJAN_E2E_APP_PORT:-8080}"
DASHBOARD_PORT="${LAHIJAN_E2E_DASHBOARD_PORT:-4173}"
INCUS_FAKE_PORT="${LAHIJAN_E2E_INCUS_FAKE_PORT:-18091}"
POWERDNS_FAKE_PORT="${LAHIJAN_E2E_POWERDNS_FAKE_PORT:-18092}"
PG_PORT="${LAHIJAN_E2E_POSTGRES_PORT:-55432}"
S3_PORT="${LAHIJAN_E2E_S3_PORT:-8334}"
FILER_PORT="${LAHIJAN_E2E_FILER_PORT:-8889}"

# PIDs + container state for teardown.
HARNESS_PID=""
APP_PID=""
DASHBOARD_PID=""
COMPOSE_UP=0

log() { echo "[e2e] $*"; }
err() { echo "[e2e] ERROR: $*" >&2; }

# wait_for_url URL NAME TIMEOUT — poll until the URL returns any HTTP
# response (status + body don't matter; we just need the server
# listening). Prints the harness/app logs on timeout so a CI failure
# has actionable context.
wait_for_url() {
	local url="$1"
	local name="$2"
	local timeout="${3:-120}"
	local deadline=$(( SECONDS + timeout ))
	log "waiting up to ${timeout}s for ${name} at ${url}"
	while [ "$SECONDS" -lt "$deadline" ]; do
		if curl -sf -o /dev/null --max-time 2 "$url"; then
			log "${name} ready"
			return 0
		fi
		sleep 1
	done
	err "timed out waiting for ${name} at ${url}"
	if [ -f /tmp/lahijan-e2e-harness.log ]; then
		err "--- last 30 lines of harness log ---"
		tail -n 30 /tmp/lahijan-e2e-harness.log >&2 || true
	fi
	if [ -f /tmp/lahijan-e2e-app.log ]; then
		err "--- last 30 lines of app log ---"
		tail -n 30 /tmp/lahijan-e2e-app.log >&2 || true
	fi
	return 1
}

cleanup() {
	local exit_code=$?
	if [ "${LAHIJAN_E2E_KEEP_UP:-0}" = "1" ]; then
		log "LAHIJAN_E2E_KEEP_UP=1; skipping teardown"
		exit $exit_code
	fi
	log "tearing down (exit_code=$exit_code)"
	[ -n "$APP_PID" ]       && kill "$APP_PID" 2>/dev/null || true
	[ -n "$DASHBOARD_PID" ] && kill "$DASHBOARD_PID" 2>/dev/null || true
	[ -n "$HARNESS_PID" ]   && kill "$HARNESS_PID" 2>/dev/null || true
	# Give processes a moment to exit cleanly, then SIGKILL.
	sleep 1
	[ -n "$APP_PID" ]       && kill -9 "$APP_PID" 2>/dev/null || true
	[ -n "$DASHBOARD_PID" ] && kill -9 "$DASHBOARD_PID" 2>/dev/null || true
	[ -n "$HARNESS_PID" ]   && kill -9 "$HARNESS_PID" 2>/dev/null || true
	if [ "$COMPOSE_UP" = "1" ]; then
		docker compose -f deployments/docker-compose.test.yml down -v --remove-orphans \
			|| log "compose down failed (continuing)"
	fi
	exit $exit_code
}
trap cleanup EXIT INT TERM

# ---- 1. Bring up the test sandbox -----------------------------------
log "bringing up test sandbox (Postgres + SeaweedFS)"
LAHIJAN_TEST_POSTGRES_HOST_PORT="$PG_PORT" \
LAHIJAN_TEST_S3_HOST_PORT="$S3_PORT" \
LAHIJAN_TEST_FILER_HOST_PORT="$FILER_PORT" \
	docker compose -f deployments/docker-compose.test.yml up -d --wait
COMPOSE_UP=1

# ---- 2. Apply migrations to the test Postgres -----------------------
log "applying migrations"
# Install golang-migrate if missing (CI runners cache it; local devs may
# not). `migrate` IS in the dev dependencies of WS-03 but we cannot rely
# on PATH on a fresh CI runner.
if ! command -v migrate >/dev/null 2>&1; then
	log "installing golang-migrate v4.19.1"
	tmpdir="$(mktemp -d)"
	curl -sSL "https://github.com/golang-migrate/migrate/releases/download/v4.19.1/migrate.linux-amd64.tar.gz" \
		| tar -xz -C "$tmpdir" migrate
	PATH="$tmpdir:$PATH"
fi
DB_URL="postgres://lahijan:lahijan@127.0.0.1:${PG_PORT}/lahijan?sslmode=disable"
migrate -path internal/app/lahijan/database/migrations -database "$DB_URL" up

# ---- 3. Start the provider fakes (Incus + PowerDNS) -----------------
log "starting fakes harness (incus + powerdns)"
go run -tags e2e ./test/e2e/harness \
	-incus-port "$INCUS_FAKE_PORT" \
	-powerdns-port "$POWERDNS_FAKE_PORT" \
	>/tmp/lahijan-e2e-harness.log 2>&1 &
HARNESS_PID=$!

# Wait for both fakes to be ready.
wait_for_url "http://127.0.0.1:${INCUS_FAKE_PORT}/1.0" "Incus fake" $WAIT_TIMEOUT
wait_for_url "http://127.0.0.1:${POWERDNS_FAKE_PORT}/api/v1/servers/localhost" "PowerDNS fake" $WAIT_TIMEOUT

# ---- 4. Build + start the Lahijan app -------------------------------
log "building Lahijan app"
go build -o dist/lahijan-e2e ./cmd/lahijan

log "starting Lahijan app on :${APP_PORT}"
LAHIJAN_ENVIRONMENT=dev \
LAHIJAN_AUTH_SIGNING_KEY=lahijan-e2e-signing-key-not-for-prod \
LAHIJAN_AUTH_SIGNING_KEY_DEFAULT=lahijan-e2e-signing-key-not-for-prod \
LAHIJAN_AUTH_EMAILVERIFICATIONREQUIRED=false \
LAHIJAN_DATABASE_HOST=127.0.0.1 \
LAHIJAN_DATABASE_PORT="$PG_PORT" \
LAHIJAN_DATABASE_NAME=lahijan \
LAHIJAN_DATABASE_USER=lahijan \
LAHIJAN_DATABASE_PASSWORD=lahijan \
LAHIJAN_DATABASE_SSLMODE=disable \
LAHIJAN_HTTP_SERVER_PORT="$APP_PORT" \
LAHIJAN_HTTP_SERVER_CORS_ENABLED=true \
LAHIJAN_SMTP_HOST=127.0.0.1 \
LAHIJAN_SMTP_PORT=2525 \
LAHIJAN_PROVIDERS_INCUS_ENABLED=true \
LAHIJAN_PROVIDERS_INCUS_REMOTEURL="http://127.0.0.1:${INCUS_FAKE_PORT}" \
LAHIJAN_PROVIDERS_POWERDNS_ENABLED=true \
LAHIJAN_PROVIDERS_POWERDNS_BASEURL="http://127.0.0.1:${POWERDNS_FAKE_PORT}" \
LAHIJAN_PROVIDERS_POWERDNS_APIKEY=test-key \
LAHIJAN_PROVIDERS_SEAWEEDFS_ENABLED=true \
LAHIJAN_PROVIDERS_SEAWEEDFS_S3ENDPOINT="http://127.0.0.1:${S3_PORT}" \
LAHIJAN_PROVIDERS_SEAWEEDFS_FILERURL="http://127.0.0.1:${FILER_PORT}" \
LAHIJAN_PROVIDERS_SEAWEEDFS_ADMINACCESSKEY=lahijan_test_admin_key \
LAHIJAN_PROVIDERS_SEAWEEDFS_ADMINSECRETKEY=lahijan_test_admin_secret \
	./dist/lahijan-e2e >/tmp/lahijan-e2e-app.log 2>&1 &
APP_PID=$!

wait_for_url "http://127.0.0.1:${APP_PORT}/healthz" "Lahijan app" $WAIT_TIMEOUT

# ---- 5. Build + serve the dashboard ---------------------------------
log "building dashboard"
npm --prefix web run build >/tmp/lahijan-e2e-web-build.log 2>&1

log "starting dashboard preview on :${DASHBOARD_PORT}"
npm --prefix web run preview -- --port "$DASHBOARD_PORT" --strictPort \
	>/tmp/lahijan-e2e-web.log 2>&1 &
DASHBOARD_PID=$!

wait_for_url "http://127.0.0.1:${DASHBOARD_PORT}/" "Dashboard" $WAIT_TIMEOUT

# ---- 6. Run Playwright ----------------------------------------------
log "installing Playwright browsers (cached on CI)"
cd test/e2e
npm ci
npx playwright install --with-deps chromium

log "running Playwright specs"
export LAHIJAN_E2E_BASE_URL="http://127.0.0.1:${DASHBOARD_PORT}"
export LAHIJAN_E2E_API_BASE_URL="http://127.0.0.1:${APP_PORT}"
HEADED_FLAG=""
if [ "${LAHIJAN_E2E_HEADED:-0}" = "1" ]; then
	HEADED_FLAG="--headed"
fi
timeout "$PLAYWRIGHT_TIMEOUT" npx playwright test $HEADED_FLAG

log "Playwright suite passed"
cd "$REPO_ROOT"

# ---- Done (cleanup runs via trap) -----------------------------------
log "e2e suite green"
