#!/usr/bin/env bash
# Lahijan — Postgres init script (runs once on first container boot).
#
# Creates the three logical databases and three application roles mandated by
# ADR-0007 (shared Postgres, separate logical DBs). Each app role is scoped to
# its own database only: Lahijan cannot touch pdns/seaweed, and vice versa.
# The PowerDNS and SeaweedFS schemas themselves are owned/created by WS-12 and
# WS-13; this script only provisions the databases and roles.
#
# This file is mounted read-only into /docker-entrypoint-initdb.d/ by
# docker-compose.dev.yml. The Postgres docker entrypoint runs .sh init files
# with PGPASSWORD already set and `psql` on PATH.
#
# Env vars are provided by docker-compose.dev.yml (sourced from .env).
set -euo pipefail

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" <<-EOSQL
    -- Lahijan application database + role.
    DO \$do\$
    BEGIN
        IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '${LAHIJAN_DB_USER}') THEN
            CREATE ROLE "${LAHIJAN_DB_USER}" LOGIN PASSWORD '${LAHIJAN_DB_PASSWORD}';
        END IF;
    END
    \$do\$;

    SELECT 'CREATE DATABASE ${LAHIJAN_DB_NAME} OWNER ${LAHIJAN_DB_USER}'
    WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '${LAHIJAN_DB_NAME}')\gexec

    GRANT ALL PRIVILEGES ON DATABASE "${LAHIJAN_DB_NAME}" TO "${LAHIJAN_DB_USER}";

    -- PowerDNS database + role (schema lands in WS-12).
    DO \$do\$
    BEGIN
        IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '${PDNS_DB_USER}') THEN
            CREATE ROLE "${PDNS_DB_USER}" LOGIN PASSWORD '${PDNS_DB_PASSWORD}';
        END IF;
    END
    \$do\$;

    SELECT 'CREATE DATABASE ${PDNS_DB_NAME} OWNER ${PDNS_DB_USER}'
    WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '${PDNS_DB_NAME}')\gexec

    GRANT ALL PRIVILEGES ON DATABASE "${PDNS_DB_NAME}" TO "${PDNS_DB_USER}";

    -- SeaweedFS filer database + role (schema lands in WS-13).
    DO \$do\$
    BEGIN
        IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '${SEAWEED_DB_USER}') THEN
            CREATE ROLE "${SEAWEED_DB_USER}" LOGIN PASSWORD '${SEAWEED_DB_PASSWORD}';
        END IF;
    END
    \$do\$;

    SELECT 'CREATE DATABASE ${SEAWEED_DB_NAME} OWNER ${SEAWEED_DB_USER}'
    WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '${SEAWEED_DB_NAME}')\gexec

    GRANT ALL PRIVILEGES ON DATABASE "${SEAWEED_DB_NAME}" TO "${SEAWEED_DB_USER}";
EOSQL

# Ensure each app role owns the public schema of its own database, so
# golang-migrate and the app can create tables without superuser grants.
for db_role in "lahijan:${LAHIJAN_DB_USER}" "pdns:${PDNS_DB_USER}" "seaweed:${SEAWEED_DB_USER}"; do
    db="${db_role%%:*}"
    role="${db_role##*:}"
    psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$db" <<-EOSQL
        ALTER SCHEMA public OWNER TO "${role}";
EOSQL
done
