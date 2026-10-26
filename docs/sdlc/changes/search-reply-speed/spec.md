# Contract

2026-10-21. See [intent](intent.md), [plan](plan.md) and
[issue #409](https://github.com/alberttwong/redis-adbc-driver/issues/409).

Preserve RESP2/RESP3 accepted result shapes, unsupported/malformed reply errors,
cursor IDs for cleanup, internal fields, duplicates and list-length reducers.
Avoid extra normalization maps where possible. Keep safe copies for byte values;
do not borrow protocol buffers or change SQL/retrieval policy.

RESP3 remains the default. Compare the pinned merged baseline and candidate
through both ADBC APIs, with explicit RESP3 native redis-py references, complete
Arrow IPC consumption and independent exact values/multiplicity validation.
Separate parser CPU/allocations from server/transport time and publish failures
and tradeoffs. No transfer-speed claim from allocations alone.

Outcome: a repeated regular-read elapsed reduction of approximately 7% on the
wide fixture, with lower client CPU on both APIs. Partition wall-time gain is
not established by the confirmation run; narrow medians are slightly higher
with overlapping ranges. See [evidence](plan.md#evidence-and-outcome).
