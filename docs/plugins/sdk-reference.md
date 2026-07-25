# SDK Reference

The Lahijan Go SDK (`github.com/avestura/lahijan/sdk-go`) wraps every host
function behind idiomatic Go APIs. This page is the complete reference.

## Packages

| Package | Purpose |
|---------|---------|
| `sdk-go/mem` | Linear-memory helpers (`Ptr`, `Read`, `Len`) |
| `sdk-go/status` | Status-code constants + typed errors |
| `sdk-go/kv` | Per-plugin durable key-value store |
| `sdk-go/config` | Admin-set plugin configuration |
| `sdk-go/events` | Event bus (emit, subscribe, unsubscribe) |
| `sdk-go/network` | Outbound HTTP (full response: status + headers + body) |
| `sdk-go/jobs` | Schedule future work via River |
| `sdk-go/api` | Register HTTP handlers under the plugin prefix |
| `sdk-go/compute` | Manage compute instances (containers + VMs) |
| `sdk-go/dns` | Manage DNS zones + records |
| `sdk-go/storage` | Manage S3 buckets |

---

## mem

```go
func Ptr(b []byte) uint32      // linear-memory offset of a slice
func Read(ptr, length uint32) []byte  // slice view over [ptr, ptr+length)
func Len(b []byte) uint32      // length as uint32 for host calls
```

## status

```go
type Code int32

const (
    Success          Code = 0
    GenericFailure   Code = -1
    Denied           Code = -2
    Unavailable      Code = -3
    InvalidMemory    Code = -4
    InvalidArgument  Code = -5
    NotFound         Code = -6
    BufferTooSmall   Code = -7
    UpstreamError    Code = -8
)

// Sentinel errors (all errors.Is-able)
var ErrPermissionDenied, ErrNotFound, ErrUnavailable, ErrBufferTooSmall, ...

func FromCode(code int32) error
```

## kv

```go
func Get(key string) ([]byte, error)
func Set(key string, value []byte, ttlMs int64) error
func Delete(key string) error
```

Auto-retries on `BufferTooSmall` (starts at 1 KiB, doubles to 256 KiB).

## config

```go
func Get(key string) ([]byte, error)
func GetString(key string) (string, error)
```

Secret keys surface as `ErrNotFound`. Auto-retries on `BufferTooSmall`.

## events

```go
func Emit(topic string, payload []byte) error
func Subscribe(topicPattern, handlerName string) error
func Unsubscribe(topicPattern, handlerName string) error
```

## network

```go
type Request struct {
    Method  string
    URL     string
    Headers map[string]string
    Body    []byte
}

type Response struct {
    StatusCode int
    Headers    map[string]string
    Body       []byte
}

func Do(req Request) (Response, error)
func Get(url string) (Response, error)
func Post(url string, body []byte) (Response, error)
func PostJSON(url string, body []byte) (Response, error)
```

Full response (status + headers + body). Auto-retries on `BufferTooSmall`.

## jobs

```go
func Schedule(exportName string, args []byte, runAtMs int64) error
```

`runAtMs` is Unix-millis; capped at `conf.wasm.max_run_at_offset` in the
future.

## api

```go
func RegisterHandler(method, path, handlerName string) error
func UnregisterHandler(method, path string) error
```

Paths mount under `/api/v1/plugins/<plugin-name>/<path>`.

## compute

```go
type Instance struct {
    ID, Name, Type, Status, ImageAlias, Description string
    StatusCode int32
    Profiles   []string
}

type CreateInstanceParams struct {
    Name, Type, ImageAlias string
    Profiles               []string
    Config                 map[string]string
}

func CreateInstance(params CreateInstanceParams) (Instance, error)
func GetInstance(id string) (Instance, error)
func ListInstances(limit, offset int32) ([]Instance, error)
func SetInstanceState(id, action string, force bool, timeoutSecs int) (Instance, error)
func DeleteInstance(id string) error
```

`action` is `"start"`, `"stop"`, `"restart"`, `"freeze"`, or `"unfreeze"`.

## dns

```go
type Zone struct {
    ID, Name, Kind, Description string
}

type Record struct {
    ID, ZoneID, Name, Type, Content string
    TTL                             int
}

type CreateZoneParams struct{ Name, Description, Kind string }
type CreateRecordParams struct{ ZoneID, Name, Type, Content string; TTL int }

func CreateZone(params CreateZoneParams) (Zone, error)
func GetZone(id string) (Zone, error)
func ListZones(limit, offset int32) ([]Zone, error)
func DeleteZone(id string) error

func CreateRecord(params CreateRecordParams) (Record, error)
func ListRecords(zoneID string, limit, offset int32) ([]Record, error)
func DeleteRecord(zoneID, recordID string) error
```

## storage

```go
type Bucket struct {
    ID, Slug, Label, Description string
}

type CreateBucketParams struct {
    Slug, Label, Description string
    QuotaBytes, QuotaObjects int64
}

func CreateBucket(params CreateBucketParams) (Bucket, error)
func GetBucket(id string) (Bucket, error)
func ListBuckets(limit, offset int32) ([]Bucket, error)
func DeleteBucket(id string) error
```
