// Package kv provides idiomatic Go access to the Lahijan per-plugin
// key-value store. The host injects the calling plugin's id from the call
// context, so a plugin cannot address another plugin's namespace.
//
// The auto-retry on BufferTooSmall is hidden inside Get: the SDK starts
// with a 1 KiB buffer and doubles it until the value fits or the host-side
// cap (256 KiB) is reached.
package kv

import (
	"github.com/avestura/lahijan/sdk-go/mem"
	"github.com/avestura/lahijan/sdk-go/status"
)

const (
	initialBufSize = 1024             // start at 1 KiB
	maxBufSize     = 256 * 1024       // matches MaxKVValueLen on the host
)

// Get reads the value for key from the plugin's KV namespace. Returns the
// raw bytes, ErrNotFound if the key is missing, or another status error if
// the call failed.
func Get(key string) ([]byte, error) {
	k := []byte(key)
	buf := make([]byte, initialBufSize)
	for {
		n := kvGet(mem.Ptr(k), mem.Len(k), mem.Ptr(buf), uint32(len(buf)))
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

// Set writes value to the plugin's KV namespace under key. ttlMs > 0 sets
// a TTL in milliseconds; 0 means no expiry.
func Set(key string, value []byte, ttlMs int64) error {
	k := []byte(key)
	code := kvSet(mem.Ptr(k), mem.Len(k), mem.Ptr(value), mem.Len(value), ttlMs)
	return status.FromCode(code)
}

// Delete removes key from the plugin's KV namespace. Succeeds even if the
// key was already absent.
func Delete(key string) error {
	k := []byte(key)
	code := kvDelete(mem.Ptr(k), mem.Len(k))
	return status.FromCode(code)
}
