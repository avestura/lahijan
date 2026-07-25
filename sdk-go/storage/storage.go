// Package storage provides idiomatic Go access to the Lahijan object
// storage service from inside a WASM plugin. Plugins can create, list,
// inspect, and delete S3 buckets — all gated by storage.bucket.*
// permissions and scoped to the plugin's tenant.
package storage

import (
	"encoding/json"

	"github.com/avestura/lahijan/sdk-go/mem"
	"github.com/avestura/lahijan/sdk-go/status"
)

// Bucket represents an S3 bucket.
type Bucket struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// CreateBucketParams carries the fields for creating a bucket.
type CreateBucketParams struct {
	Slug         string `json:"slug"`
	Label        string `json:"label"`
	Description  string `json:"description"`
	QuotaBytes   int64  `json:"quota_bytes"`
	QuotaObjects int64  `json:"quota_objects"`
}

const (
	bufStartSize = 4096
	bufMaxSize   = 256 * 1024
)

// CreateBucket creates a new S3 bucket.
func CreateBucket(params CreateBucketParams) (Bucket, error) {
	raw, err := callWithRetry(bucketCreate, mustMarshal(params))
	if err != nil {
		return Bucket{}, err
	}
	var b Bucket
	return b, json.Unmarshal(raw, &b)
}

// GetBucket fetches a bucket by ID.
func GetBucket(id string) (Bucket, error) {
	raw, err := callWithRetry(bucketGet, mustMarshal(map[string]string{"id": id}))
	if err != nil {
		return Bucket{}, err
	}
	var b Bucket
	return b, json.Unmarshal(raw, &b)
}

// ListBuckets lists buckets in the plugin's tenant.
func ListBuckets(limit, offset int32) ([]Bucket, error) {
	raw, err := callWithRetry(bucketList, mustMarshal(map[string]int32{"limit": limit, "offset": offset}))
	if err != nil {
		return nil, err
	}
	var buckets []Bucket
	return buckets, json.Unmarshal(raw, &buckets)
}

// DeleteBucket deletes a bucket by ID.
func DeleteBucket(id string) error {
	_, err := callWithRetry(bucketDelete, mustMarshal(map[string]string{"id": id}))
	return err
}

func callWithRetry(fn func(argsPtr, argsLen, bufPtr, bufCap uint32) int32, args []byte) ([]byte, error) {
	buf := make([]byte, bufStartSize)
	for {
		n := fn(mem.Ptr(args), mem.Len(args), mem.Ptr(buf), uint32(len(buf)))
		if status.Code(n) == status.BufferTooSmall {
			if len(buf) >= bufMaxSize {
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

func mustMarshal(v any) []byte {
	out, _ := json.Marshal(v)
	return out
}
