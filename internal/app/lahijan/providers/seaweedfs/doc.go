// Package seaweedfs is the Lahijan driver that translates Lahijan's object
// storage domain into SeaweedFS S3 + Filer API calls. It implements the
// providers.Provider interface and exposes every SeaweedFS surface Lahijan's
// storage module needs (buckets, IAM credentials, quotas, pre-signed URLs,
// Filer metadata).
//
// Per ADR-0027 this driver is a hybrid:
//
//   - The S3 data + control plane (CreateBucket, DeleteBucket, HeadBucket,
//     ListBuckets, PutObject, GetObject) and pre-signed URL generation go
//     through the AWS SDK for Go v2 `service/s3` client. SigV4 request
//     signing + presign come from the SDK; we do not implement that
//     cryptography ourselves.
//   - The SeaweedFS Filer metadata API (IAM identities, per-bucket quota
//     configuration, status probes) is handled by a thin internal REST
//     client, mirroring the WS-11 / WS-12 (Incus / PowerDNS) driver shape.
//   - Tests inject operations-interface mocks so unit tests do not need to
//     implement the S3 XML wire protocol. Integration tests in WS-22 will
//     exercise the real HTTP path against a real SeaweedFS container.
//
// # Layout
//
//   - client.go — Provider struct, Config, NewClient, AWS SDK + Filer HTTP wiring.
//   - types.go  — request/response types mirroring the SeaweedFS S3 + Filer
//     shapes used by this driver.
//   - tracing.go — package-local OpenTelemetry tracer (per ADR-0016).
//   - errors.go — sentinel errors + REST error envelope decoder.
//   - naming.go — bucket-naming convention enforcement (ADR-0011).
//   - buckets.go — bucket CRUD + HeadBucket + ListBuckets via the AWS SDK client.
//   - iam.go — per-user S3 credential mint / revoke / rotate via the Filer.
//   - quotas.go — per-bucket quota enforcement (Filer-side config).
//   - presign.go — pre-signed URL generation for limited-time direct access.
//   - filer.go — direct Filer HTTP API (status, volume info, metadata CRUD).
//   - events.go — SeaweedFS does not emit events; we synthesize them on
//     every change so the WASM event bus sees a uniform stream.
//   - provider.go — providers.Provider impl (Name / Ping / Capabilities).
//   - fake/server.go — in-memory fake satisfying the operations interfaces
//     for unit tests.
//
// # Concurrency
//
// All methods on *Provider are safe for concurrent use. The underlying AWS
// SDK S3 client and the net/http.Client are goroutine-safe; per-call state
// lives only on the call stack. The capabilities cache is guarded by a
// sync.RWMutex.
//
// # Tracing
//
// Every public method opens a span via the package tracer. Span names
// follow the convention "seaweedfs.<area>.<verb>" (e.g.
// "seaweedfs.bucket.create"). The canonical bucket name (when present) is
// recorded as the "seaweedfs.bucket" attribute so a slow SeaweedFS-side
// operation can be cross-referenced with SeaweedFS' own access logs.
//
// # Tenant mapping
//
// Per ADR-0011 and pillar 2, every Lahijan tenant maps to one or more
// buckets in SeaweedFS. The driver itself never carries a tenant_id; the
// storage service (WS-16) consults the `buckets` table to translate a
// tenant context into the canonical bucket name (formed as
// "<tenant-uuid>-<slug>" per ADR-0011) before calling the driver. Per
// pillar 1 the Name() string "seaweedfs" is internal-only; end users see
// "object storage" / "S3".
package seaweedfs
