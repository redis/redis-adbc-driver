# First-valid sequence investigation: specification

Source/date/status: [intent](intent.md). The opt-in single-endpoint Software test uses one real pinned catalog and three-segment descriptor, a pre-acquired execution transport and production maintenance. Native helper sequences are explicitly distinct from SQL timing.

For eight rounds in alternating order, issue three fresh valid calls before and after no proof, one execution-client proof, three execution-client proofs or three maintenance-client proofs. Use the original FT.INFO/ZCARD helper and server pin validation. Check the exact bounded request count and fail on any refusal/unknown reply; do not replay. Keep normal SQL type/result oracles before/after the diagnostic, including extrema/NULL/empty/deleted rows.

Record only command shapes, logical route labels, elapsed times, numeric pool counters and connection RESP byte counts. Distinguish client identity from main/dedicated pipeline pools. Use existing diagnostics ownership checks and clear the temporary socket counter after every call. Socket elapsed time is not server CPU or a pure foreground/additive decomposition. SLOWLOG and its threshold are read-only and optional; record unavailable/empty evidence explicitly and never publish arguments, physical keys, SQL, client identities or native IDs. Do not alter server settings.

Bound the helper diagnostic with a 45-second context. Verify Software 4x QPF before running exclusively; close/join processes, pins, transports and clients, and clean fixture data before releasing the slot.

Review: [PR #440](https://github.com/redis/redis-adbc-driver/pull/440).
