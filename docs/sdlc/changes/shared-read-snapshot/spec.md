# Shared read snapshot specification

Date: 2026-11-10. Status: implemented, awaiting review/release.
Issue: https://github.com/redis/redis-adbc-driver/issues/365.
Base: `85509485dbc07b2e2e0ea9e1b8e08bd11a290587`.

The [public contract](../../../shared-read-snapshots.md) specifies the exact
options, supported operations, boundaries, authority, ownership, timeouts,
retention and retries. One authenticated existing protocol-17 pin binds the
connection; each statement/stream borrows a reference. Explicit release removes
the connection reference; readers keep theirs. Fixed expiry invalidates all
references. No persisted state format or exported capability is introduced.

Verify complete metadata equality and literal expected values across independent
publications; fresh ordinary statements must see newer values. Verify failure
without fallback, bounded admission, parent/child cleanup, cancellation, process
death and restricted-user revocation. Test native C/Python separately from Go.
Flight SQL remains explicitly unsupported. Release selection for Airbyte remains
a maintainer release gate, not a claimed released capability.

Risks: stale authority, accidental write paths, pin release races, retention
backpressure, silent live fallback and overclaiming transport/release evidence.
See [intent](intent.md), [plan](plan.md), and [verification](verification.md).
