# dns-record-hook

A sample Lahijan plugin that listens for `dns.record.*` events and calls
an external webhook with the record payload. Use it as a starting point
for "DNS change → external system" integrations (CMDB sync, audit
pipeline, Slack notifier, etc.).

## Permissions declared

| Slug | Why |
|------|-----|
| `events.listen:dns.record.*` | Get every record change (created/updated/deleted). |
| `network.outbound:example.com` | POST the rendered payload to the configured webhook host. |
| `config.read:dns-record-hook` | Read the admin-set target URL. |

## Build

```sh
make build
```

## Configuration

The admin sets a single config key, `webhook_url`, pointing at the
external endpoint that should receive the events. The host part of the
URL must be in the `network.outbound:*` grant (or be reachable from the
default `network.outbound:example.com` permission shipped in this
sample). Update the manifest to match the host you actually need.

```sh
curl -X POST "http://localhost:3000/api/v1/admin/plugins/$PLUGIN_ID/config" \
     -H 'Content-Type: application/json' \
     -H 'Cookie: lahijan_session=<your-admin-session>' \
     -H 'X-Tenant-Id: <tenant-uuid>' \
     --data '{"key":"webhook_url","value":"https://example.com/webhook","is_secret":true}'
```
