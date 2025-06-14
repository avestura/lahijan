# Lahijan Cloud Platform

Lahijan is a simple platform for all your infrastructure needs including servers, containers, and dns.

## Configuration

Lahijan is designed with a zero-configuration philosophy in mind, however you can config it
in a way that works out for you. The configuration is loaded in this order:

- [default configuration](./internal/app/lahijan/conf/.lahijan.conf.default.yaml)
- `/etc/lahijan/.lahijan.conf.yaml`
- `$HOME/.lahijan/.lahijan.conf.yaml` 
- `./lahijan.conf.yaml`
- environment variables prefixed with `LAHIJAN`
  - for example `LAHIJAN_HTTP_SERVER_PORT` for setting `http.server.port`
- CLI arguments e.g. `lahijan --http.server.port 5000`
