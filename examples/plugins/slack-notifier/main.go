// Command slack-notifier is a sample Lahijan plugin that listens for
// compute.instance.* events and POSTs a JSON payload to a Slack
// incoming-webhook URL.
//
// The plugin follows the conventions in ADR-0023 (plain wasm32-unknown-
// unknown, no WASI imports) and ADR-0024 ((ptr, len) host-function ABI).
// Every host call returns an i32 status code; non-zero codes are logged
// but never crash the plugin — the runtime traps only on context
// cancellation or memory faults.
//
// The webhook URL is read from the admin-set config key `webhook_url`
// (declared secret in the manifest so it is encrypted at rest). The
// plugin never logs the URL; it only uses it as the outbound request
// target.
package main

import (
	"encoding/json"
	"unsafe"
)

// — Host function imports ————————————————————————————————————

//go:wasm-import lahijan_config get func(keyPtr, keyLen, bufPtr, bufCap uint32) int32
//go:wasm-import lahijan_network http_request func(methodPtr, methodLen, urlPtr, urlLen, headersPtr, headersLen, bodyPtr, bodyLen, respBufPtr, respBufCap uint32) int32

// — Entrypoints ——————————————————————————————————————————————

// on_event is called by the runtime for every matching event. The
// manifest declares `events.listen:compute.instance.*`, so the runtime
// invokes on_event with the event payload as JSON in the plugin's
// linear memory.
//
//export on_event
func on_event(payloadPtr, payloadLen uint32) {
	payload := readMem(payloadPtr, payloadLen)

	// Read the admin-set webhook URL. The buffer is sized generously; a
	// Slack webhook URL is ~90 chars but we leave headroom for any
	// proxy/gateway variants.
	var urlBuf [512]byte
	urlLen := configGet([]byte("webhook_url"), urlBuf[:])
	if urlLen <= 0 {
		// config_get returns a negative code on missing/secret-locked
		// keys. The plugin cannot do anything useful without a URL, so
		// it just returns. The host's OTel span records the call.
		return
	}
	webhookURL := string(urlBuf[:urlLen])

	body := renderSlackMessage(payload)
	httpPostJSON(webhookURL, body)
}

// — Helpers ———————————————————————————————————————————————————

// renderSlackMessage builds the Slack incoming-webhook JSON body from
// the event payload. The payload is the raw event JSON (see
// eventbus.Event.Metadata); we wrap it in Slack's standard "text" +
// "blocks" shape so the message renders nicely in the channel.
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

// httpPostJSON issues a POST request with Content-Type: application/json.
// The host function takes raw (ptr, len) pairs; we allocate scratch space
// in the linear memory and never reuse it across calls (the host reads
// the bytes synchronously before returning).
func httpPostJSON(url string, body []byte) {
	method := []byte("POST")
	contentType := []byte("Content-Type: application/json")
	http_request(
		ptrOf(method), uint32(len(method)),
		ptrOf([]byte(url)), uint32(len(url)),
		ptrOf(contentType), uint32(len(contentType)),
		ptrOf(body), uint32(len(body)),
		0, 0, // no response buffer; we discard the body
	)
}

// configGet reads a config key. The host writes up to cap bytes into
// buf and returns the number written (or a negative status code).
func configGet(key []byte, buf []byte) int32 {
	return config_get(ptrOf(key), uint32(len(key)), ptrOf(buf), uint32(len(buf)))
}

// — Memory helpers ———————————————————————————————————————————

// readMem returns a Go slice view over [ptr, ptr+length) in linear memory.
// The slice is valid for the duration of the calling export only.
func readMem(ptr, length uint32) []byte {
	if length == 0 {
		return []byte{}
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
}

// ptrOf returns the linear-memory offset of a slice's backing array.
// TinyGo represents slice headers as (ptr, len, cap); the data pointer
// is a raw i32 into linear memory.
func ptrOf(b []byte) uint32 {
	if len(b) == 0 {
		return 0
	}
	return uint32(uintptr(unsafe.Pointer(&b[0])))
}

// main is required by TinyGo; never called by the host.
func main() {}
