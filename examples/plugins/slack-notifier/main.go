// Command slack-notifier is a sample Lahijan plugin that listens for
// compute.instance.* events and POSTs a JSON payload to a Slack
// incoming-webhook URL.
//
// This version uses the Lahijan Go SDK (sdk-go) — no unsafe.Pointer,
// no (ptr, len) juggling, no status-code switches. Compare with the
// previous raw-import version (~136 LOC) — this is ~45 LOC of business
// logic.
//
// The webhook URL is read from the admin-set config key `webhook_url`
// (declared secret in the manifest so it is encrypted at rest). The
// plugin never logs the URL; it only uses it as the outbound request
// target.
package main

import (
	"encoding/json"

	"github.com/avestura/lahijan/sdk-go/config"
	"github.com/avestura/lahijan/sdk-go/mem"
	"github.com/avestura/lahijan/sdk-go/network"
)

// on_event is called by the runtime for every matching event. The
// manifest declares events.listen:compute.instance.*, so the runtime
// invokes on_event with the event payload as JSON in the plugin's
// linear memory.
//
//export on_event
func on_event(payloadPtr, payloadLen uint32) {
	payload := mem.Read(payloadPtr, payloadLen)

	// Read the admin-set webhook URL. config.GetString returns
	// ErrNotFound on missing or secret-locked keys — the plugin
	// cannot do anything useful without a URL, so it just returns.
	webhookURL, err := config.GetString("webhook_url")
	if err != nil {
		return
	}

	body := renderSlackMessage(payload)
	_, _ = network.PostJSON(webhookURL, body)
}

// renderSlackMessage builds the Slack incoming-webhook JSON body from
// the event payload.
func renderSlackMessage(eventPayload []byte) []byte {
	var event struct {
		Topic      string `json:"topic"`
		TenantID   string `json:"tenant_id"`
		ResourceID string `json:"resource_id"`
		ActorType  string `json:"actor_type"`
	}
	_ = json.Unmarshal(eventPayload, &event) // best-effort decode

	msg := map[string]any{
		"text": "*" + event.Topic + "*",
		"blocks": []map[string]any{
			{
				"type": "section",
				"text": map[string]any{
					"type": "mrkdwn",
					"text": "*" + event.Topic + "* triggered by *" + event.ActorType + "*",
				},
			},
			{
				"type": "context",
				"elements": []map[string]any{
					{"type": "mrkdwn", "text": "tenant: `" + event.TenantID + "`  resource: `" + event.ResourceID + "`"},
				},
			},
		},
	}
	out, _ := json.Marshal(msg)
	return out
}

// main is required by TinyGo so the module has a start function. The
// runtime never calls it; real work happens in the exported entrypoints.
func main() {}
