# Lahijan — Incus sidecar.
#
# Per ADR-0005 Incus runs on the host (it needs kernel access); the Lahijan
# app talks to it over the Unix socket. In dev (`make run`) the Lahijan app
# itself runs on the host and connects directly to the socket — no container
# is needed.
#
# In prod (Lahijan runs in a container), this sidecar container is added to
# the Lahijan service in docker-compose to give Lahijan a stable path to the
# socket (typically `/var/lib/incus/unix.socket` inside the Lahijan
# container). The Incus daemon on the host binds the socket on its own
# filesystem; this sidecar mounts the host's socket into the Lahijan
# container via a bind-mount.
#
# This file is documentation + a snippet operators paste into the prod
# compose stack. It is NOT a complete compose file on its own.

# Example: how the incus-client sidecar mounts the host socket.
#
# services:
#   lahijan:
#     image: ghcr.io/avestura/lahijan:<version>
#     depends_on: [incus-client]
#     environment:
#       LAHIJAN_PROVIDERS_INCUS_ENABLED: "true"
#       LAHIJAN_PROVIDERS_INCUS_SOCKET_PATH: /var/lib/incus/unix.socket
#     volumes:
#       - incus_socket:/var/lib/incus
#
#   incus-client:
#     image: ghcr.io/lxc/incus:6.0  # client only; just needs the socket
#     restart: unless-stopped
#     # The daemon runs on the host; this container only forwards the socket.
#     # Mount the host's Incus socket directory read-only into the sidecar.
#     volumes:
#       - /var/lib/incus:/var/lib/incus:ro
#     # Pin to a sidecar that does nothing but keep the mount alive. The
#     # real work happens via the socket; the sidecar's only job is to
#     # namespace the mount.
#     command: ["sleep", "infinity"]
#     healthcheck:
#       test: ["CMD", "test", "-S", "/var/lib/incus/unix.socket"]
#       interval: 10s
#       timeout: 3s
#       retries: 6
#       start_period: 5s
#
# volumes:
#   incus_socket:

# Required host setup (run once on the Incus host, not in any container):
#
#   1. Install Incus per https://linuxcontainers.org/incus/docs/main/installing/
#   2. Initialise the daemon with the preseed:
#        incus admin init --preseed < deployments/incus/preseed.yaml
#   3. Grant the user the Lahijan app runs as the `incus-admin` group, or use
#      a dedicated `lahijan` system user with membership in the incus group.
#   4. Confirm `incus list` works as that user before bringing up the stack.
