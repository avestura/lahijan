// Package config provides idiomatic access to the plugin's admin-set
// configuration. Values flagged secret:true in the manifest's config_schema
// are encrypted at rest and surface as ErrNotFound to the plugin (the admin
// UI shows them; the plugin never sees the plaintext).
package config

import (
	"github.com/avestura/lahijan/sdk-go/mem"
	"github.com/avestura/lahijan/sdk-go/status"
)

const (
	initialBufSize = 1024        // start at 1 KiB
	maxBufSize     = 64 * 1024   // matches MaxConfigValueLen on the host
)

// Get reads the raw JSON value for key from the plugin's admin-set config.
// Returns ErrNotFound if the key is missing or marked secret.
func Get(key string) ([]byte, error) {
	k := []byte(key)
	buf := make([]byte, initialBufSize)
	for {
		n := configGet(mem.Ptr(k), mem.Len(k), mem.Ptr(buf), uint32(len(buf)))
		switch status.Code(n) {
		case status.NotFound:
			return nil, status.ErrNotFound
		case status.BufferTooSmall:
			if len(buf) >= maxBufSize {
				return nil, status.ErrBufferTooSmall
			}
			buf = make([]byte, len(buf)*2)
			continue
		}
		if err := status.FromCode(n); err != nil {
			return nil, err
		}
		return buf[:n], nil
	}
}

// GetString reads a config key and returns it as a string. The underlying
// value is raw JSON; for a JSON string value ("hello") this returns the
// string content. For other JSON types, it returns the raw JSON text.
func GetString(key string) (string, error) {
	raw, err := Get(key)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
