// Command dns-record-hook is a sample Lahijan plugin that forwards
// dns.record.* events to an external webhook URL. The URL is read from
// the admin-set `webhook_url` config key (encrypted at rest as a
// secret).
package main

import (
	"unsafe"
)

// — Host function imports ————————————————————————————————————

//go:wasm-import lahijan_config get func(keyPtr, keyLen, bufPtr, bufCap uint32) int32
//go:wasm-import lahijan_network http_request func(methodPtr, methodLen, urlPtr, urlLen, headersPtr, headersLen, bodyPtr, bodyLen, respBufPtr, respBufCap uint32) int32

// — Entrypoints ——————————————————————————————————————————————

// on_event is invoked by the runtime on every matching dns.record.*
// event. The plugin reads the webhook URL, then POSTs the raw event
// payload to it.
//
//export on_event
func on_event(payloadPtr, payloadLen uint32) {
	payload := readMem(payloadPtr, payloadLen)

	var urlBuf [512]byte
	urlLen := configGet([]byte("webhook_url"), urlBuf[:])
	if urlLen <= 0 {
		return
	}
	webhookURL := string(urlBuf[:urlLen])

	method := []byte("POST")
	contentType := []byte("Content-Type: application/json")
	http_request(
		ptrOf(method), uint32(len(method)),
		ptrOf([]byte(webhookURL)), uint32(len(webhookURL)),
		ptrOf(contentType), uint32(len(contentType)),
		ptrOf(payload), uint32(len(payload)),
		0, 0,
	)
}

// configGet reads a config key into buf. Returns the number of bytes
// written, or a negative status code on missing/secret-locked.
func configGet(key []byte, buf []byte) int32 {
	return config_get(ptrOf(key), uint32(len(key)), ptrOf(buf), uint32(len(buf)))
}

// — Memory helpers ———————————————————————————————————————————

func readMem(ptr, length uint32) []byte {
	if length == 0 {
		return []byte{}
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
}

func ptrOf(b []byte) uint32 {
	if len(b) == 0 {
		return 0
	}
	return uint32(uintptr(unsafe.Pointer(&b[0])))
}

// main is required by TinyGo; never called by the host.
func main() {}
