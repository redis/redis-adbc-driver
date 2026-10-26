# Partition API implementation plan

Date: 2026-10-20. Author: Codex. Requestor: user in this chat.
Source commit: a08573bdc40c07bca5bc3ccf9cee1e59cbc6490c.
Status: implementation and local verification complete; human review pending.

See [intent](intent.md), [spec](spec.md), and [verification](validation.md).

1. Add `go/partitions.go`: strict scope, metadata identity, descriptors, range
   planning and a streamed worker reader. Replace statement/connection stubs.
2. Expose the statement partition-count option; reset C partition release fields.
3. Add unit, Redis integration and Python native-driver/process tests.
4. Document lifecycle, consistency, supported query/deployment boundaries and
   a runnable process-pool example. Preserve concurrent Dask working changes.
5. Run focused tests, full Go regression suite, native build and Python C/API
   tests. Review lifecycle, query safety and evidence; record results here.

No automated upstream messaging, release, or merge is part of this change.
