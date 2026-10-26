# Implementation and verification

See [intent](intent.md) and [spec](spec.md).

Add backup_version.verify_snapshot_head and four focused tests, integrate it into survivor reuse, and add a backup-only retry controller.

Evidence: reproduced version-specific HEAD 403; operator exact-version HEAD passed; runner current-object HEAD returned matching saved version, bytes and checksum. Four tests pass: direct version, exact fallback, rejection of replaced/changed objects, and no fallback for unrelated errors. Compilation and diff checks pass. Live backup completion remains pending; no restore success is claimed. Agent review checked identity and failure propagation; human PR review pending.
