# slack-notifier

A sample Lahijan plugin that listens for `compute.instance.*` events and
posts a message to a Slack incoming-webhook URL.

The webhook URL is read from the admin-set `webhook_url` config key
(declared as a secret so it is encrypted at rest). The plugin never logs
the URL; it only POSTs the rendered message body to it.

## Permissions declared

| Slug | Why |
|------|-----|
| `events.listen:compute.instance.*` | Receive every instance lifecycle event. |
| `network.outbound:hooks.slack.com` | POST the rendered message to Slack. |
| `config.read:slack-notifier` | Read the admin-set webhook URL. |

## Build

```sh
make build
```

Produces `plugin.wasm`. Upload via the admin API or copy into the
marketplace directory (see `examples/plugins/marketplace/`).

## Install

```sh
curl -F 'wasm=@plugin.wasm;type=application/wasm' \
     -F 'manifest=@lahijan.manifest.yaml;type=text/yaml' \
     -H 'Cookie: lahijan_session=<your-admin-session>' \
     -H 'X-Tenant-Id: <tenant-uuid>' \
     http://localhost:3000/api/v1/admin/plugins/upload

# Grant each declared permission.
PLUGIN_ID=<id-from-previous-response>
for slug in \
    'events.listen%3Acompute.instance.*' \
    'network.outbound%3Ahooks.slack.com' \
    'config.read%3Aslack-notifier' ; do
  curl -X POST "http://localhost:3000/api/v1/admin/plugins/$PLUGIN_ID/permissions/$slug/grant" \
       -H 'Cookie: lahijan_session=<your-admin-session>' \
       -H 'X-Tenant-Id: <tenant-uuid>'
done

# Set the webhook URL (secret) and enable.
curl -X POST "http://localhost:3000/api/v1/admin/plugins/$PLUGIN_ID/config" \
     -H 'Content-Type: application/json' \
     -H 'Cookie: lahijan_session=<your-admin-session>' \
     -H 'X-Tenant-Id: <tenant-uuid>' \
     --data '{"key":"webhook_url","value":"https://hooks.slack.com/services/AAA/BBB/CCC","is_secret":true}'

curl -X POST "http://localhost:3000/api/v1/admin/plugins/$PLUGIN_ID/enable" \
     -H 'Cookie: lahijan_session=<your-admin-session>' \
     -H 'X-Tenant-Id: <tenant-uuid>'
```

The plugin starts receiving events the next time a `compute.instance.*`
event fires. Slack notifications appear within ~1 second.
