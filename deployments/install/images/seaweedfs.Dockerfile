# lahijan-seaweedfs: SeaweedFS with the filer metadata-store configuration baked
# in (PostgreSQL), used for the master, volume, filer and S3 containers.
#
# Build context: the repository root.
FROM chrislusf/seaweedfs:3.99
COPY deployments/seaweedfs/filer.toml /etc/seaweedfs/filer.toml
COPY deployments/install/images/lahijan-env.sh /usr/local/bin/lahijan-env
ENTRYPOINT ["/usr/local/bin/lahijan-env", "/entrypoint.sh"]
