// wasm_test_modules.go holds hand-encoded WASM modules used by runtime_test.go.
//
// We hand-encode rather than depending on wat2wasm or a third-party Wat
// parser so the test suite has zero external build deps and runs on every
// platform Lahijan supports. Each helper returns a fresh []byte so a test
// can mutate without affecting parallel tests.
//
// The byte layouts follow the WebAssembly 1.0 binary format
// (https://www.w3.org/TR/wasm-core-1/). Each section is annotated with the
// equivalent Wat so a reader can follow along.

package runtime

// addModule is a minimal (i32, i32) -> i32 module that exports "add" and a
// 1-page memory (64 KiB, max 1 page). Wat:
//
//	(module
//	  (memory (export "memory") 1 1)
//	  (func (export "add") (param i32 i32) (result i32)
//	    local.get 0
//	    local.get 1
//	    i32.add))
func addModule() []byte {
	return []byte{
		// magic + version
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,

		// section 1 (type): length 7 ; 1 type, functype (i32 i32) -> (i32)
		0x01, 0x07, 0x01,
		0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f,

		// section 3 (function): length 2 ; 1 function, signature index 0
		0x03, 0x02, 0x01, 0x00,

		// section 5 (memory): length 4 ; 1 memory, flag=01 (min+max), min=1, max=1
		0x05, 0x04, 0x01, 0x01, 0x01, 0x01,

		// section 7 (export): length 16 ; 2 exports
		//   "memory" (6 chars) kind=02 (memory) index=00
		//   "add"    (3 chars) kind=00 (func)   index=00
		0x07, 0x10, 0x02,
		0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00,
		0x03, 0x61, 0x64, 0x64, 0x00, 0x00,

		// section 10 (code): length 9 ; 1 function, body size=7
		//   locals count=0
		//   local.get 0 ; local.get 1 ; i32.add ; end
		0x0a, 0x09, 0x01,
		0x07,
		0x00,
		0x20, 0x00,
		0x20, 0x01,
		0x6a,
		0x0b,
	}
}

// loopModule is a minimal module exporting "loop_forever" that loops
// infinitely. Used to test the per-call timeout. Wat:
//
//	(module
//	  (memory (export "memory") 1 1)
//	  (func (export "loop_forever")
//	    (loop $l
//	      br $l)))
func loopModule() []byte {
	return []byte{
		// magic + version
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,

		// section 1 (type): length 4 ; 1 type, functype () -> ()
		0x01, 0x04, 0x01,
		0x60, 0x00, 0x00,

		// section 3 (function): length 2 ; 1 function, signature 0
		0x03, 0x02, 0x01, 0x00,

		// section 5 (memory): length 4 ; min=1 max=1
		0x05, 0x04, 0x01, 0x01, 0x01, 0x01,

		// section 7 (export): length 25 (0x19) ; 2 exports
		//   "memory"       (6 chars)  kind=02 (memory) index=00
		//   "loop_forever" (12 chars) kind=00 (func)   index=00
		0x07, 0x19, 0x02,
		0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00,
		0x0c, 0x6c, 0x6f, 0x6f, 0x70, 0x5f, 0x66, 0x6f, 0x72, 0x65, 0x76, 0x65, 0x72, 0x00, 0x00,

		// section 10 (code): length 9 ; 1 function, body size=7
		//   locals count=0
		//   loop (empty blocktype 0x40) ; br 0 ; end-loop ; end-func
		//   (opcode 0x02 = block, 0x03 = loop; we want loop here.)
		0x0a, 0x09, 0x01,
		0x07,
		0x00,
		0x03, 0x40,
		0x0c, 0x00,
		0x0b,
		0x0b,
	}
}

// bigMemoryModule declares an exported memory with min=1, max=600 pages
// (~37.5 MiB). With a cap below 600 pages the compile-time check rejects
// the module. Wat:
//
//	(module
//	  (memory (export "memory") 1 600))
//
// 600 in LEB128 = 0xD8 0x04 (verify: 0x58 + (0x04 << 7) = 88 + 512 = 600).
func bigMemoryModule() []byte {
	return []byte{
		// magic + version
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,

		// section 5 (memory): length 5 ; 1 memory, flag=01 (min+max), min=1, max=600 LEB128
		0x05, 0x05, 0x01, 0x01, 0x01, 0xd8, 0x04,

		// section 7 (export): length 10 (0x0a) ; 1 export "memory" -> memory 0
		0x07, 0x0a, 0x01,
		0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00,
	}
}

// importModule declares an import from a host module named "env". The
// runtime (per ADR-0023) registers NO host imports in WS-10a, so
// instantiation of this module must fail. Used to assert "a plugin with no
// granted permissions loads but cannot call any host func" — actually the
// stronger property "a plugin that needs an import cannot instantiate at
// all in WS-10a". Wat:
//
//	(module
//	  (import "env" "log" (func $log (param i32)))
//	  (func (export "run") i32.const 42 call $log))
func importModule() []byte {
	return []byte{
		// magic + version
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,

		// section 1 (type): length 5 ; 1 type, functype (i32) -> ()
		0x01, 0x05, 0x01,
		0x60, 0x01, 0x7f, 0x00,

		// section 2 (import): length 11 (0x0b) ; 1 import
		//   module "env" (3 chars) ; name "log" (3 chars) ; kind 00 (func) ; type index 0
		0x02, 0x0b, 0x01,
		0x03, 0x65, 0x6e, 0x76,
		0x03, 0x6c, 0x6f, 0x67,
		0x00, 0x00,

		// section 3 (function): length 2 ; 1 function (the run export), signature 0
		0x03, 0x02, 0x01, 0x00,

		// section 7 (export): length 7 ; 1 export "run" -> function index 1
		//   (function index 0 is the imported "env.log"; the export is index 1)
		0x07, 0x07, 0x01,
		0x03, 0x72, 0x75, 0x6e, 0x00, 0x01,

		// section 10 (code): length 8 ; 1 function, body size=6
		//   locals count=0
		//   i32.const 42 ; call 0 ; end
		0x0a, 0x08, 0x01,
		0x06,
		0x00,
		0x41, 0x2a,
		0x10, 0x00,
		0x0b,
	}
}
