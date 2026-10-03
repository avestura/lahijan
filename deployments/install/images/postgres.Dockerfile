# lahijan-postgres: PostgreSQL with Lahijan's first-boot initialisation baked in
# (three databases and roles, plus the PowerDNS schema), so the install needs no
# files next to the compose file.
#
# Build context: the repository root.
FROM postgres:16-alpine
COPY deployments/postgres/init.sh /docker-entrypoint-initdb.d/01-init.sh
COPY deployments/powerdns/init.sh /docker-entrypoint-initdb.d/02-pdns-schema.sh
COPY deployments/powerdns/schema.pgsql.sql /powerdns/schema.pgsql.sql
COPY deployments/install/images/lahijan-env.sh /usr/local/bin/lahijan-env
RUN chmod 755 /usr/local/bin/lahijan-env
ENTRYPOINT ["/usr/local/bin/lahijan-env", "docker-entrypoint.sh"]
CMD ["postgres"]
