# Point-validation response isolation: specification

Source/requestor: [intent](intent.md). Execution/evidence: [plan](plan.md).

Use a disposable Redis Software instance with independently verified active and
effective 4× QPF, six Search workers, mnp/32 routing, noeviction, strict TLS and
RESP3. Run exclusively, outside all sibling builds and live work.

Retain one real pin and a three-segment immutable descriptor. Run ten command
arms: none; three FT.INFO/ZCARD pairs pipelined; three FT.INFO pipelined; three or
six ZCARD pipelined; three FT.INFO serial; three pairs serial; six ZCARD serial;
three repeated FT.INFO on one index pipelined; and three pairs pipelined through
the maintenance client. The other proof arms use the execution client. Distinct
clients may each use separate dedicated pipeline pools; no physical-connection
identity is inferred.

For every arm issue three fresh PING or native pin-valid requests before and
after probes. Alternate the entire case/response order across eight rounds:
160 sequences and 960 response requests. Check PONG, exact directory cardinality
and index identity/readiness/document count. Use NoRetry commands and reject all
command, outer-pipeline and deadline failures. Require exact helper request counts.
Normal SQL before/after checks complete Arrow INT64/STRING results, int64 extrema,
values above 2^53, NULL, empty strings and deletion visibility.

Archive all observations, source/binary hashes and any unsuccessful run rather
than discarding anomalies. Publish only command categories, routes, counters and
durations. Keep command arguments, keys, endpoints, credentials and infrastructure
identities private. Socket counters are connection-scoped and may include
concurrent maintenance I/O. This helper diagnostic excludes normal per-statement
transport lifecycle and is neither production SQL latency nor server CPU evidence.

Version 2 retains the original 160-sequence version separately and adds one/two
FT.INFO groups, fixed grouping into one/two-index chunks, complete sequence time
and proof reply-byte counters: 256 sequences. A frozen baseline/candidate
A1/B1/B2/A2 width diagnostic checks 3/24 public columns and 1/2/3/8 pending probes;
eight probes repeat real immutable indexes from a three-segment table and must
never be labeled an eight-segment SQL fixture. Production compaction stays on.

The later response-routing diagnostic compares scalar/single-command-pipeline
PING on execution/maintenance clients, including the native library's existing
dedicated-pool opt-out. It tests native FT.INFO and exact application-owned GET
values around 8 KiB. A separate frozen standalone Go executable uses only
go-redis RESP3/TLS GET/PING with owned expiring keys: no ADBC, SQL, Search, Lua or
managed metadata. Independent client/topology controls remain separately labeled.
