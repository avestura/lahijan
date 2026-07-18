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

// encodeI32Const emits the i32.const <value> instruction. The value is
// encoded as SIGNED LEB128 because the WASM spec says i32.const takes
// a signed value. For values 0..63 the signed + unsigned encodings are
// identical; for value 64 the signed encoding needs a continuation
// byte (bit 6 is the sign bit in the last byte of signed LEB128).
func encodeI32Const(v uint32) []byte {
	out := []byte{0x41} // i32.const opcode
	return append(out, encodeLEB128Signed(int64(v))...)
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

// emitConfigGetModule builds a WASM module that calls config.get with
// the supplied key. The plugin supplies a 4 KiB response buffer at
// offset 4096; the host writes the value (if any) there.
func emitConfigGetModule(key string) []byte {
	const keyOff, bufOff, bufCap = 0, 4096, 4096
	const memPages = 1

	typeSec := section(0x01, []byte{
		0x02,
		0x60, 0x04, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7f, // config.get
		0x60, 0x00, 0x01, 0x7f, // run
	})
	importBody := []byte{0x01}
	importBody = append(importBody, encodeString("lahijan_config")...)
	importBody = append(importBody, encodeString("get")...)
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
	body = append(body, encodeI32Const(keyOff)...)
	body = append(body, encodeI32Const(uint32(len(key)))...)
	body = append(body, encodeI32Const(bufOff)...)
	body = append(body, encodeI32Const(bufCap)...)
	body = append(body, 0x10, 0x00)
	body = append(body, 0x0b)
	fnBody := append(encodeLEB128(uint32(len(body))), body...)
	codeSec := section(0x0a, append([]byte{0x01}, fnBody...))

	dataBody := []byte{0x01}
	dataBody = append(dataBody, 0x00)
	dataBody = append(dataBody, encodeI32Const(keyOff)...)
	dataBody = append(dataBody, 0x0b)
	dataBody = append(dataBody, encodeLEB128(uint32(len(key)))...)
	dataBody = append(dataBody, []byte(key)...)
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

// emitHTTPRequestModule builds a WASM module that calls
// lahijan_network.http_request with method + url only (no headers, no body).
// Sufficient for the network permission-gate test.
func emitHTTPRequestModule(method, url string) []byte {
	const methodOff, urlOff, headersOff, bodyOff = 0, 64, 1024, 2048
	const memPages = 1

	typeSec := section(0x01, []byte{
		0x02,
		// type 0: (i32 i32 i32 i32 i32 i32 i32 i32) -> i32 -- http_request
		0x60, 0x08, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7f,
		0x60, 0x00, 0x01, 0x7f, // type 1: () -> i32 -- run
	})
	importBody := []byte{0x01}
	importBody = append(importBody, encodeString("lahijan_network")...)
	importBody = append(importBody, encodeString("http_request")...)
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
	body = append(body, encodeI32Const(methodOff)...)
	body = append(body, encodeI32Const(uint32(len(method)))...)
	body = append(body, encodeI32Const(urlOff)...)
	body = append(body, encodeI32Const(uint32(len(url)))...)
	body = append(body, encodeI32Const(headersOff)...)
	body = append(body, encodeI32Const(0)...) // no headers
	body = append(body, encodeI32Const(bodyOff)...)
	body = append(body, encodeI32Const(0)...) // no body
	body = append(body, 0x10, 0x00)
	body = append(body, 0x0b)
	fnBody := append(encodeLEB128(uint32(len(body))), body...)
	codeSec := section(0x0a, append([]byte{0x01}, fnBody...))

	dataBody := []byte{0x02}
	dataBody = append(dataBody, 0x00)
	dataBody = append(dataBody, encodeI32Const(methodOff)...)
	dataBody = append(dataBody, 0x0b)
	dataBody = append(dataBody, encodeLEB128(uint32(len(method)))...)
	dataBody = append(dataBody, []byte(method)...)
	dataBody = append(dataBody, 0x00)
	dataBody = append(dataBody, encodeI32Const(urlOff)...)
	dataBody = append(dataBody, 0x0b)
	dataBody = append(dataBody, encodeLEB128(uint32(len(url)))...)
	dataBody = append(dataBody, []byte(url)...)
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

// emitRegisterHandlerModule builds a WASM module that calls
// api.register_handler(method, path, handler).
func emitRegisterHandlerModule(method, path, handler string) []byte {
	const methodOff, pathOff, handlerOff = 0, 64, 1024
	const memPages = 1

	typeSec := section(0x01, []byte{
		0x02,
		// type 0: (i32 i32 i32 i32 i32 i32) -> i32 -- register_handler
		0x60, 0x06, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7f,
		0x60, 0x00, 0x01, 0x7f, // type 1: () -> i32 -- run
	})
	importBody := []byte{0x01}
	importBody = append(importBody, encodeString("lahijan_api")...)
	importBody = append(importBody, encodeString("register_handler")...)
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
	body = append(body, encodeI32Const(methodOff)...)
	body = append(body, encodeI32Const(uint32(len(method)))...)
	body = append(body, encodeI32Const(pathOff)...)
	body = append(body, encodeI32Const(uint32(len(path)))...)
	body = append(body, encodeI32Const(handlerOff)...)
	body = append(body, encodeI32Const(uint32(len(handler)))...)
	body = append(body, 0x10, 0x00)
	body = append(body, 0x0b)
	fnBody := append(encodeLEB128(uint32(len(body))), body...)
	codeSec := section(0x0a, append([]byte{0x01}, fnBody...))

	dataBody := []byte{0x03}
	// method
	dataBody = append(dataBody, 0x00)
	dataBody = append(dataBody, encodeI32Const(methodOff)...)
	dataBody = append(dataBody, 0x0b)
	dataBody = append(dataBody, encodeLEB128(uint32(len(method)))...)
	dataBody = append(dataBody, []byte(method)...)
	// path
	dataBody = append(dataBody, 0x00)
	dataBody = append(dataBody, encodeI32Const(pathOff)...)
	dataBody = append(dataBody, 0x0b)
	dataBody = append(dataBody, encodeLEB128(uint32(len(path)))...)
	dataBody = append(dataBody, []byte(path)...)
	// handler
	dataBody = append(dataBody, 0x00)
	dataBody = append(dataBody, encodeI32Const(handlerOff)...)
	dataBody = append(dataBody, 0x0b)
	dataBody = append(dataBody, encodeLEB128(uint32(len(handler)))...)
	dataBody = append(dataBody, []byte(handler)...)
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

// emitScheduleModule builds a WASM module that calls jobs.schedule
// with the supplied export name, args payload, and run_at timestamp.
func emitScheduleModule(name, args string, runAtMS int64) []byte {
	const nameOff, argsOff = 0, 256
	const memPages = 1

	typeSec := section(0x01, []byte{
		0x02,
		// type 0: (i32 i32 i32 i32 i64) -> i32 -- schedule
		0x60, 0x05, 0x7f, 0x7f, 0x7f, 0x7f, 0x7e, 0x01, 0x7f,
		0x60, 0x00, 0x01, 0x7f,
	})
	importBody := []byte{0x01}
	importBody = append(importBody, encodeString("lahijan_jobs")...)
	importBody = append(importBody, encodeString("schedule")...)
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
	body = append(body, encodeI32Const(nameOff)...)
	body = append(body, encodeI32Const(uint32(len(name)))...)
	body = append(body, encodeI32Const(argsOff)...)
	body = append(body, encodeI32Const(uint32(len(args)))...)
	body = append(body, encodeI64Const(runAtMS)...)
	body = append(body, 0x10, 0x00)
	body = append(body, 0x0b)
	fnBody := append(encodeLEB128(uint32(len(body))), body...)
	codeSec := section(0x0a, append([]byte{0x01}, fnBody...))

	dataBody := []byte{0x02}
	dataBody = append(dataBody, 0x00)
	dataBody = append(dataBody, encodeI32Const(nameOff)...)
	dataBody = append(dataBody, 0x0b)
	dataBody = append(dataBody, encodeLEB128(uint32(len(name)))...)
	dataBody = append(dataBody, []byte(name)...)
	dataBody = append(dataBody, 0x00)
	dataBody = append(dataBody, encodeI32Const(argsOff)...)
	dataBody = append(dataBody, 0x0b)
	dataBody = append(dataBody, encodeLEB128(uint32(len(args)))...)
	dataBody = append(dataBody, []byte(args)...)
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
