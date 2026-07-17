# WS-29 · S3 Lifecycle / Versioning / Object Lock (DEFERRED)

```
Status: deferred
Phase: 7
Depends: WS-13 (SeaweedFS provider)
Unblocks: —
```

> **Deferred past MVP.** WS-13/16 ship bucket CRUD + pre-signed URLs; this
> WS adds S3 lifecycle policies, versioning, and object lock for compliance.

## Goal

Bring SeaweedFS' S3 feature surface up to parity with what users expect from
a "real" S3: lifecycle rules, versioning, object lock + retention +
legal-hold.

## Scope (when work begins)

- Bucket versioning: enable/suspend; list versions; restore deleted version
- Lifecycle rules: expiration, transition to lower tier, abort incomplete
  multipart, NVP noncurrent version expiration
- Object lock: retention modes (GOVERNANCE / COMPLIANCE), legal hold, WORM
- UI: per-bucket versioning toggle, lifecycle rule editor, object lock policy
- Audit: every privileged operation logged
- WASM hooks: `storage.object.deleted`, `storage.lifecycle.transitioned`

## Required reading (when work begins)

- `/AGENTS.md`
- WS-13, WS-16 docs (the provider + module this WS extends)
- [SeaweedFS lifecycle redesign](https://github.com/seaweedfs/seaweedfs/blob/master/S3_LIFECYCLE_REDESIGN.md)
- [AWS S3 Object Lock docs](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lock.html)

## Notes

- Check current SeaweedFS support for each feature at the time work begins —
  some features may require a specific version or still be in flux.
