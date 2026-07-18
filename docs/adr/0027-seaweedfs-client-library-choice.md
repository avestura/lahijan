# ADR-0027: SeaweedFS client library choice — AWS SDK v2 (S3) + thin HTTP (Filer/IAM)

- **Status:** Accepted
- **Date:** 2026-07-18
- **Deciders:** maintainer

## Context

WS-13 ("SeaweedFS Provider") builds the driver under
`internal/app/lahijan/providers/seaweedfs/`. Unlike Incus (WS-11) and PowerDNS
(WS-12), SeaweedFS exposes an **already-standardised** wire protocol: the
Amazon S3 API. The WS-13 brief explicitly recommends "AWS SDK for Go v2 —
official, Apache-2 license" in its Open Questions, leaving the door open to
alternatives (minio-go).

The Incus (ADR-0025) and PowerDNS (ADR-0026) ADRs both chose thin internal
HTTP clients because their backends ship no first-class Go SDK and the wire
protocols are daemon-specific. The SeaweedFS situation is the opposite: the
S3 wire protocol is well-documented and stable, SigV4 request signing is
non-trivial to hand-roll, and pre-signed URLs are a core deliverable that
re-uses the same SigV4 algorithm — so a thick SDK is genuinely useful here.

The non-negotiables that bear on this choice (`/AGENTS.md`,
"Non-negotiables"):

1. **No new dependencies without checking license + pattern fit.** AWS SDK
   for Go v2 is Apache-2.0, modular, and the canonical S3 client. minio-go
   is also Apache-2.0 but pulls a larger single-package surface and is less
   common outside MinIO-specific deployments.
2. **Tests stub the backends via httptest.** AWS SDK v2 is a thick client
   (it owns SigV4 signing, XML marshalling, retry, endpoint resolution).
   Driving the SDK against a hand-rolled httptest fake would require
   implementing the S3 XML wire protocol in the fake, which is materially
   more work than the Incus/PowerDNS fakes and adds little value because
   the SigV4 path is exactly the part we trust the SDK with.

Options considered:

- **Option A — AWS SDK v2 + thin HTTP for Filer/IAM, with interface
  injection for tests.** Pros: SigV4 + presign come for free from the SDK;
  Filer/IAM stay thin HTTP (mirrors WS-11/WS-12); tests are fast because
  the S3 surface is mocked at the operations interface boundary, not at
  the XML wire boundary. Cons: we own small wrappers around the SDK and
  around the Filer/IAM REST endpoints; tests do not exercise the SDK's
  own HTTP transport (mitigation: WS-22 integration tests will run the
  full stack against real SeaweedFS).
- **Option B — AWS SDK v2 + httptest fake implementing the S3 XML wire
  protocol.** Pros: pattern-uniform with WS-11/WS-12. Cons: the S3 wire
  fake is ~500 lines of XML marshalling for the subset of bucket/object
  operations we use, plus IAM XML if we go that route; high implementation
  cost; brittle to SDK version bumps that tweak the wire shape.
- **Option C — Thin internal REST client over S3 (hand-roll SigV4).**
  Pros: zero new runtime deps. Cons: SigV4 is ~150 lines of careful
  crypto (HMAC-SHA256 over canonical request); pre-signed URL generation
  duplicates the same crypto; future S3 features (multipart, checksums)
  would all need hand-rolled support. The cost outweighs the benefit when
  the official SDK is Apache-2.0 and modular.
- **Option D — minio-go client.** Pros: single self-contained package,
  SigV4 + presign built in. Cons: pulls the entire minio-go surface
  (multipart manager, s3-setter, etc.); the AWS SDK v2 is more modular
  (we import only `service/s3`); the WS-13 brief explicitly defaults to
  AWS SDK v2.

## Decision

WS-13 implements the SeaweedFS driver as a hybrid:

1. **S3 data + control plane operations** (CreateBucket, DeleteBucket,
   HeadBucket, ListBuckets, PutObject, GetObject) use the AWS SDK for Go
   v2 `service/s3` client. SigV4 request signing and pre-signed URL
   generation are delegated to the SDK.
2. **Pre-signed URLs** use the SDK's `s3.NewPresignClient` so users get
   standard SigV4 query-string-signed URLs that any S3-compatible tool
   (aws-cli, rclone, mc, SDKs) accepts.
3. **SeaweedFS Filer metadata + IAM + per-bucket quota configuration**
   use a thin internal REST client (mirrors the Incus/PowerDNS pattern in
   ADR-0025 / ADR-0026). SeaweedFS stores IAM identities and per-bucket
   quotas in Filer metadata paths under `/etc/seaweedfs/...`; the driver
   reads and writes those paths directly via the Filer's HTTP API.
4. **Tests mock at the operations-interface boundary**, not at the wire
   boundary. The driver declares narrow internal interfaces
   (`s3Operations`, `filerOperations`) and injects implementations in
   `NewClient`. Production wires real AWS SDK + thin HTTP clients; tests
   wire in-memory fakes in `providers/seaweedfs/fake/`. The fake does
   NOT implement the S3 XML wire protocol — it implements the
   operations interface directly, so each test is a few hundred
   nanoseconds per assertion instead of an HTTP round-trip.

This deliberately diverges from the WS-11/WS-12 httptest pattern, because
the right test boundary for a thick SDK client is the operations
interface, not the wire. WS-22 (integration test harness) will exercise
the real HTTP path end-to-end against a real SeaweedFS container.

## Consequences

- **Positive:** SigV4 + pre-signed URLs come from the SDK — we do not
  implement HMAC-SHA256 cryptography ourselves.
- **Positive:** test surface is small and fast; no S3 XML wire-format
  fake to maintain.
- **Positive:** IAM + quota configuration stays thin HTTP, matching the
  WS-11/WS-12 shape for the parts of the driver that talk to
  SeaweedFS-specific endpoints.
- **Negative:** we add the AWS SDK v2 dependency tree to `go.mod`. The
  SDK is modular so the actual import set is small (`aws`,
  `credentials`, `service/s3`); the indirect dep graph is well-maintained
  and Apache-2.0 licensed throughout.
- **Negative:** unit tests do not exercise the SDK's HTTP transport.
  Mitigation: the integration test harness in WS-22 brings up a real
  SeaweedFS container and runs the storage module end-to-end.

## Compliance

- `internal/app/lahijan/providers/seaweedfs/client.go` constructs the
  AWS SDK S3 client + the thin HTTP Filer client and exposes them
  through internal interfaces; `buckets.go`, `iam.go`, `quotas.go`,
  `presign.go`, `filer.go` call only through those interfaces.
- The only new runtime modules added by WS-13 are AWS SDK v2 packages
  (`github.com/aws/aws-sdk-go-v2/aws`,
  `.../credentials`, `.../service/s3`) plus their existing transitive
  deps. No new SDK or framework is introduced.
- `internal/app/lahijan/providers/seaweedfs/fake/server.go` is the
  in-memory fake every unit test in the package reuses. It implements
  the operations interfaces directly (no XML marshalling, no HTTP).
- All provider methods open an OpenTelemetry span via the package-local
  tracer in `tracing.go` (per ADR-0016 "Full OTel").
- The driver never carries `tenant_id`; the tenant → bucket mapping is
  enforced via the bucket-naming convention `<tenant-uuid>-<slug>`
  (ADR-0011) and consulted by the storage service (WS-16) before any
  driver call.
- Per pillar 1 the Name() string "seaweedfs" is internal-only; end
  users see "object storage" / "S3".

## References

- ADR-0011 (direct S3 for data + Lahijan for control plane)
- ADR-0007 (shared Postgres — the `seaweed` logical DB the Filer reads
  from)
- ADR-0016 (full OTel — every provider call traced)
- ADR-0025 (Incus client library choice — the sister pattern this ADR
  mirrors for the Filer/IAM HTTP path)
- ADR-0026 (PowerDNS client library choice — same)
- [SeaweedFS S3 API wiki](https://github.com/seaweedfs/seaweedfs/wiki/Amazon-S3-API)
- [AWS SDK for Go v2](https://aws.github.io/aws-sdk-go-v2/docs/)
- WS-13 (SeaweedFS provider), WS-16 (storage module), WS-22
  (integration test harness)
