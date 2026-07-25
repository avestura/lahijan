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

# The schema is applied above by the POSTGRES_USER (superuser), so every
# table + sequence it creates is owned by the superuser — NOT by the
# PDNS_DB_USER that the pdns daemon connects as. Postgres then hides
# those tables from PDNS_DB_USER ("relation does not exist") because the
# role has no privileges on them. 01-init.sh already transferred the
# public SCHEMA ownership to PDNS_DB_USER (so it has USAGE), but table
# privileges are separate from schema ownership. Grant them explicitly
# so the daemon can read/write its tables on a fresh boot.
echo "Granting table+sequence privileges to '${PDNS_DB_USER:-pdns}'..."
psql -v ON_ERROR_STOP=1 \
     --username "$POSTGRES_USER" \
     --dbname "${PDNS_DB_NAME:-pdns}" <<-EOSQL
    GRANT ALL PRIVILEGES ON ALL TABLES    IN SCHEMA public TO "${PDNS_DB_USER:-pdns}";
    GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO "${PDNS_DB_USER:-pdns}";
EOSQL

echo "PowerDNS schema applied."
