// wasmemit.go produces hand-encoded WASM modules used by the
// host-function tests. Each helper returns a []byte that imports a
// lahijan_* host function and exports a "run" function that calls it.
// We hand-encode rather than depending on wat2wasm or a third-party
// Wat parser so the test suite has zero external build deps and runs
// on every platform Lahijan supports (mirrors the approach in
// internal/app/lahijan/wasm/runtime/wasm_test_modules.go).
//
// The byte layouts follow the WebAssembly 1.0 binary format
// (https://www.w3.org/TR/wasm-core-1/).

package hostfuncs

// wasmMagic is the WASM module preamble (\0asm + version 1).
var wasmMagic = []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

// encodeString encodes a length-prefixed (LEB128) string for use in
// import module/name sections + export name sections.
func encodeString(s string) []byte {
	out := make([]byte, 0, len(s)+1)
	out = append(out, byte(len(s)))
	out = append(out, []byte(s)...)
	return out
}

// encodeI32Const emits the i32.const <value> instruction (LEB128).
func encodeI32Const(v uint32) []byte {
	out := []byte{0x41} // i32.const opcode
	return append(out, encodeLEB128(v)...)
}

// encodeI64Const emits the i64.const <value> instruction.
func encodeI64Const(v int64) []byte {
	out := []byte{0x42} // i64.const opcode
	return append(out, encodeLEB128Signed(v)...)
}

// encodeLEB128 encodes an unsigned 32-bit value as LEB128.
func encodeLEB128(v uint32) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			break
		}
	}
	return out
}

// encodeLEB128Signed encodes a signed 64-bit value as LEB128.
func encodeLEB128Signed(v int64) []byte {
	var out []byte
	more := true
	for more {
		b := byte(v & 0x7f)
		v >>= 7
		signBit := b & 0x40
		if (v == 0 && signBit == 0) || (v == -1 && signBit != 0) {
			more = false
		} else {
			b |= 0x80
		}
		out = append(out, b)
	}
	return out
}

// section builds a WASM section from its id + payload.
func section(id byte, body []byte) []byte {
	out := []byte{id}
	out = append(out, encodeLEB128(uint32(len(body)))...)
	return append(out, body...)
}

// emitKVSetModule builds a WASM module that:
//   - imports "lahijan_kv" "set" with signature (i32 i32 i32 i32 i64) -> i32
//   - declares a 1-page memory (export "memory")
//   - initialises bytes [0..N] = key, [256..256+N] = val
//   - exports "run" that calls kv_set(keyOff, len(key), valOff, len(val), ttlMS)
//
// The plugin's plugin_id is supplied by the test via runtime.Instance.Call.
func emitKVSetModule(key, val string) []byte {
	return emitKVSetModuleTTL(key, val, 0)
}

// emitKVSetModuleTTL is the configurable-ttl variant.
func emitKVSetModuleTTL(key, val string, ttlMS int64) []byte {
	const keyOff, valOff = 0, 256
	const memPages = 1

	// type section: 2 types
	//   type 0: (i32 i32 i32 i32 i64) -> i32  -- kv.set
	//   type 1: () -> i32                     -- run
	typeBody := []byte{
		0x02, // 2 types
		0x60, // functype
		0x05, 0x7f, 0x7f, 0x7f, 0x7f, 0x7e, // 5 params i32 i32 i32 i32 i64
		0x01, 0x7f, // 1 result i32
		0x60, // functype
		0x00, // 0 params
		0x01, 0x7f, // 1 result i32
	}
	typeSec := section(0x01, typeBody)

	// import: "lahijan_kv" "set"
	importBody := []byte{0x01}
	importBody = append(importBody, encodeString("lahijan_kv")...)
	importBody = append(importBody, encodeString("set")...)
	importBody = append(importBody, 0x00, 0x00) // kind=func, type idx 0
	importSec := section(0x02, importBody)

	// function: 1 (the "run" export), signature 1
	funcSec := section(0x03, []byte{0x01, 0x01})

	// memory: 1, limits flag=0x01 (min+max), min=max=memPages
	memSec := section(0x05, []byte{0x01, 0x01, byte(memPages), byte(memPages)})

	// exports: memory + run
	exportBody := []byte{0x02}
	exportBody = append(exportBody, encodeString("memory")...)
	exportBody = append(exportBody, 0x02, 0x00) // memory, idx 0
	exportBody = append(exportBody, encodeString("run")...)
	exportBody = append(exportBody, 0x00, 0x01) // func, idx 1 (after imported kv.set at idx 0)
	exportSec := section(0x07, exportBody)

	// code: 1 function body
	body := []byte{0x00} // 0 locals
	body = append(body, encodeI32Const(keyOff)...)
	body = append(body, encodeI32Const(uint32(len(key)))...)
	body = append(body, encodeI32Const(valOff)...)
	body = append(body, encodeI32Const(uint32(len(val)))...)
	body = append(body, encodeI64Const(ttlMS)...)
	body = append(body, 0x10, 0x00) // call 0 (kv.set)
	body = append(body, 0x0b)       // end
	fnBody := append(encodeLEB128(uint32(len(body))), body...)
	codeSec := section(0x0a, append([]byte{0x01}, fnBody...))

	// data: 2 segments (key + val)
	dataBody := []byte{0x02}
	dataBody = append(dataBody, 0x00) // segment flags: active, memory 0
	dataBody = append(dataBody, encodeI32Const(keyOff)...)
	dataBody = append(dataBody, 0x0b) // end offset expr
	dataBody = append(dataBody, encodeLEB128(uint32(len(key)))...)
	dataBody = append(dataBody, []byte(key)...)
	dataBody = append(dataBody, 0x00)
	dataBody = append(dataBody, encodeI32Const(valOff)...)
	dataBody = append(dataBody, 0x0b)
	dataBody = append(dataBody, encodeLEB128(uint32(len(val)))...)
	dataBody = append(dataBody, []byte(val)...)
	dataSec := section(0x0b, dataBody)

	out := append([]byte{}, wasmMagic...)
	out = append(out, typeSec...)
	out = append(out, importSec...)
	out = append(out, funcSec...)
	out = append(out, memSec...)
	out = append(out, exportSec...)
	out = append(out, codeSec...)
	out = append(out, dataSec...)
	return out
}

// emitEmitEventModule builds a WASM module that calls events.emit
// with the supplied topic + payload bytes.
func emitEmitEventModule(topic, payload string) []byte {
	const topicOff, payloadOff = 0, 256
	const memPages = 1

	typeSec := section(0x01, []byte{
		0x02, // 2 types
		0x60, 0x04, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7f, // type 0: (i32 i32 i32 i32) -> i32 -- events.emit
		0x60, 0x00, 0x01, 0x7f, // type 1: () -> i32 -- run
	})

	importBody := []byte{0x01}
	importBody = append(importBody, encodeString("lahijan_events")...)
	importBody = append(importBody, encodeString("emit")...)
	importBody = append(importBody, 0x00, 0x00)
	importSec := section(0x02, importBody)

	funcSec := section(0x03, []byte{0x01, 0x01})
	memSec := section(0x05, []byte{0x01, 0x01, byte(memPages), byte(memPages)})

	exportBody := []byte{0x02}
	exportBody = append(exportBody, encodeString("memory")...)
	exportBody = append(exportBody, 0x02, 0x00)
	exportBody = append(exportBody, encodeString("run")...)
	exportBody = append(exportBody, 0x00, 0x01)
	exportSec := section(0x07, exportBody)

	body := []byte{0x00}
	body = append(body, encodeI32Const(topicOff)...)
	body = append(body, encodeI32Const(uint32(len(topic)))...)
	body = append(body, encodeI32Const(payloadOff)...)
	body = append(body, encodeI32Const(uint32(len(payload)))...)
	body = append(body, 0x10, 0x00)
	body = append(body, 0x0b)
	fnBody := append(encodeLEB128(uint32(len(body))), body...)
	codeSec := section(0x0a, append([]byte{0x01}, fnBody...))

	dataBody := []byte{0x02}
	dataBody = append(dataBody, 0x00)
	dataBody = append(dataBody, encodeI32Const(topicOff)...)
	dataBody = append(dataBody, 0x0b)
	dataBody = append(dataBody, encodeLEB128(uint32(len(topic)))...)
	dataBody = append(dataBody, []byte(topic)...)
	dataBody = append(dataBody, 0x00)
	dataBody = append(dataBody, encodeI32Const(payloadOff)...)
	dataBody = append(dataBody, 0x0b)
	dataBody = append(dataBody, encodeLEB128(uint32(len(payload)))...)
	dataBody = append(dataBody, []byte(payload)...)
	dataSec := section(0x0b, dataBody)

	out := append([]byte{}, wasmMagic...)
	out = append(out, typeSec...)
	out = append(out, importSec...)
	out = append(out, funcSec...)
	out = append(out, memSec...)
	out = append(out, exportSec...)
	out = append(out, codeSec...)
	out = append(out, dataSec...)
	return out
}
