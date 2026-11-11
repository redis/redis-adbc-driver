# Native client wrapper specification

Date: 2026-11-11. Author: Codex. Status: initial implementation for review.
Source and request: [intent](intent.md). Execution: [plan](plan.md).

The shared Go primitive acquires a normal committed snapshot (or borrows its
explicit connection binding), loads exact table metadata and verifies existing
descriptor/index readiness proofs, resolves an internal row ID using strict
directory membership and newest-version/tombstone semantics, projects public
fields with rename/default/NULL rules, and validates the pin before returning.
No SQL parser, planner or executor is called. Reuse existing store methods and
execution transport rather than implement a new storage protocol.

Python owns the Go reader through a versioned private JSON C ABI. Binary values
are base64 encoded; output allocations are freed in finally blocks. Calls are
serialized. Snapshot contexts own separate clients and close on every exit.
Missing rows differ from all-NULL rows; permission and corruption errors must
not become successful empty replies. Raw stored bytes are not SQL typed values.

The full [public contract](../../../native-client.md) defines command semantics,
limits and exclusions. Existing backend ACL/TLS/RESP3 and snapshot retention
requirements apply. No persisted storage format changes are introduced.

Acceptance: Go and actual Python/native-library tests cover row identity,
NULL/empty/binary values and exact integers, projection, updates/deletes,
rename/default semantics, old views during publications, cleanup on consumer
failure, invalid/closed views, cancellation and missing images. Record actual
Software/QPF evidence and any untested authorization/topology behavior in
[verification](verification.md).
