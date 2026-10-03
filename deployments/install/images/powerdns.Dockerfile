# lahijan-powerdns: PowerDNS Authoritative with Lahijan's configuration template
# baked in.
#
# Build context: the repository root.
FROM powerdns/pdns-auth-49:4.9.3
COPY deployments/powerdns/pdns.prod.conf /etc/powerdns/pdns.conf
COPY deployments/powerdns/templates.d/lahijan.j2 /etc/powerdns/templates.d/lahijan.j2
COPY deployments/install/images/lahijan-env.sh /usr/local/bin/lahijan-env
ENTRYPOINT ["/usr/local/bin/lahijan-env", "/usr/bin/tini", "--", "/usr/local/sbin/pdns_server-startup"]
