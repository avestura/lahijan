//go:build tinygo

package config

//go:wasm-import lahijan_config get
func configGet(keyPtr, keyLen, bufPtr, bufCap uint32) int32
