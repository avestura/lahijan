# lahijan-web: the dashboard plus the Caddy reverse proxy in front of Lahijan,
# so the install needs no web files or proxy configuration next to the compose
# file.
#
# Build context: the repository root.
FROM node:22-alpine AS build
WORKDIR /src
COPY web/package.json web/package-lock.json web/
RUN cd web && npm ci
COPY web web
COPY api api
RUN cd web && npm run build

FROM caddy:2.8-alpine
COPY deployments/install/Caddyfile /etc/caddy/Caddyfile
COPY --from=build /src/web/dist /srv/web
