# lahijan-migrate: applies the database schema migrations that ship with this
# version of Lahijan. It runs on every `up` and does nothing when the schema is
# already current.
#
# Build context: the repository root.
FROM migrate/migrate:v4.19.1
COPY internal/app/lahijan/database/migrations /migrations
COPY deployments/install/images/lahijan-env.sh /usr/local/bin/lahijan-env
ENTRYPOINT ["/usr/local/bin/lahijan-env", "sh", "-c", "exec migrate -path=/migrations -database=\"postgres://lahijan:${LAHIJAN_DB_PASSWORD}@postgres:5432/lahijan?sslmode=disable\" up"]
