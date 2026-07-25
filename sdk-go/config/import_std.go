//go:build !tinygo

package config

var configGet = func(keyPtr, keyLen, bufPtr, bufCap uint32) int32 {
	panic("config.configGet: requires TinyGo (this stub is for go vet/go test only)")
}
