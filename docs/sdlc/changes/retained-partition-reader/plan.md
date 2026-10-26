# Plan

2026-10-21. Author: Codex; requestor: Albert Wong. Base: main 0a84794.
Status: private reader complete and locally verified; maintainer review pending.

1. Completed bounded uncompressed IPC framing/schema/record validation and zero-copy message decoding.
2. Completed private incremental reader, reserved portable version-2 descriptor, persisted reader policy loading and stream-owner integration.
3. Completed independent-process exact replay, read-only ACL, batching, cancellation/owner-close races and expiry/eviction/corruption tests on standalone and Cluster.
4. Preserve evidence, remove owned fixtures, open a component PR. Continue with reader-only ADBC bootstrap, bounded global coordinator/public opt-in execution, explicit revocation and broader layouts. None of the four runtime issues closes with this component.

See [intent](intent.md), [spec](spec.md), [evidence](evidence.md).
