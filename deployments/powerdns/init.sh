#!/usr/bin/env bash
# Lahijan — PowerDNS schema bootstrap (runs once on first Postgres boot).
#
# The shared Postgres container created the pdns database + user in
# 01-init.sh; this script applies PDNS' upstream gpgsql schema to that
# database. The schema lives next to this script in schema.pgsql.sql
# (copied verbatim from PDNS' upstream sources so we control the version).
#
# This file is mounted read-only into the postgres container's
# /docker-entrypoint-initdb.d/ as 02-pdns-schema.sh. The Postgres docker
# entrypoint runs every .sh init file with PGPASSWORD already set and psql
# on PATH.
#
# SeaweedFS' schema will land as 03-* in WS-13.
set -euo pipefail

# The PDNS schema must run against the pdns database (not the default
# $POSTGRES_DB), so we pass --dbname explicitly.
echo "Applying PowerDNS gpgsql schema to database '${PDNS_DB_NAME:-pdns}'..."
psql -v ON_ERROR_STOP=1 \
     --username "$POSTGRES_USER" \
     --dbname "${PDNS_DB_NAME:-pdns}" \
     --file /powerdns/schema.pgsql.sql

echo "PowerDNS schema applied."
