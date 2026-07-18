// Command autoscaler-stub is a sample Lahijan plugin that counts
// compute.instance.cpu_high events and emits a synthetic
// plugin.autoscaler.tick event when the count crosses a configurable
// threshold.
//
// The plugin grounds a future real autoscaler: the same event-listen +
// KV-counter pattern can drive actual scaling decisions once Phase 4
// (compute module, WS-14) lands the compute.instance.* host functions.
// For now the plugin demonstrates the events + KV surface in a useful,
// self-contained loop.
package main

import (
	"encoding/json"
	"unsafe"
)

// — Host function imports ————————————————————————————————————

//go:wasm-import lahijan_kv set func(keyPtr, keyLen, valPtr, valLen uint32, ttlMs int64) int32
//go:wasm-import lahijan_kv get func(keyPtr, keyLen, bufPtr, bufCap uint32) int32
//go:wasm-import lahijan_events emit func(topicPtr, topicLen, payloadPtr, payloadLen uint32) int32

// — Constants ————————————————————————————————————————————————

// The counter key lives in the "metrics" KV namespace; the manifest
// declares kv.read:metrics + kv.write:metrics so the host enforcer
// permits these calls.
const counterKey = "cpu_high_count"

// Default threshold when the admin has not set the config key. The
// plugin reads the threshold fresh on every event so an admin can tune
// it without re-uploading the plugin.
const defaultThreshold = 3

// — Entrypoints ——————————————————————————————————————————————

// on_event is the runtime-called entrypoint. The payload is the
// cpu_high event's metadata (JSON).
//
//export on_event
func on_event(payloadPtr, payloadLen uint32) {
	// 1. Read the current counter (best-effort; missing = 0).
	var buf [32]byte
	n := kvGet([]byte(counterKey), buf[:])
	count := 0
	if n > 0 {
		// Parse the decimal counter. We tolerate any trailing garbage.
		for _, c := range buf[:n] {
			if c < '0' || c > '9' {
				break
			}
			count = count*10 + int(c-'0')
		}
	}

	// 2. Increment + persist.
	count++
	countBytes := []byte(itoa(count))
	kvSet([]byte(counterKey), countBytes, 0)

	// 3. If we crossed the threshold, emit a tick event + reset.
	if count >= defaultThreshold {
		tickPayload, _ := json.Marshal(map[string]any{
			"topic":     "plugin.autoscaler.tick",
			"trigger":   "compute.instance.cpu_high",
			"count":     count,
			"event":     json.RawMessage(readMem(payloadPtr, payloadLen)),
		})
		eventsEmit("plugin.autoscaler.tick", tickPayload)
		kvSet([]byte(counterKey), []byte("0"), 0)
	}
}

// — Helpers ———————————————————————————————————————————————————

// itoa renders a non-negative int as ASCII. We avoid pulling in strconv
// to keep the wasm module small and stdlib-free (per ADR-0023).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// kvGet / kvSet / eventsEmit are thin wrappers that translate between
// Go slices and the (ptr, len) host ABI.
func kvGet(key, buf []byte) int32 {
	return get(ptrOf(key), uint32(len(key)), ptrOf(buf), uint32(len(buf)))
}
func kvSet(key, val []byte, ttlMs int64) int32 {
	return set(ptrOf(key), uint32(len(key)), ptrOf(val), uint32(len(val)), ttlMs)
}
func eventsEmit(topic string, payload []byte) int32 {
	t := []byte(topic)
	return emit(ptrOf(t), uint32(len(t)), ptrOf(payload), uint32(len(payload)))
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
