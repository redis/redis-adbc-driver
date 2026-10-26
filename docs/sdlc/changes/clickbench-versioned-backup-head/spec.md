# Contract

See [intent](intent.md) and [plan](plan.md).

First request metadata for the saved version. Only AccessDenied/403 allows a current-object HEAD fallback. Both paths require an identical non-null version ID, byte count and SHA256 metadata. A replacement object, missing version, changed size/hash, or other S3 error must fail. Record the verification method in the manifest.

Backup-only retry requires the preserved quiescent API checkpoint and nine valid targeted corrections. Preserve the failed manifest separately; never rerun timed scans or alter IAM. Restore still verifies downloaded snapshot bytes.
