# Bounded typed export: contract proposal and recommendation

2026-11-03, for [#195](https://github.com/redis/redis-adbc-driver/issues/195).
Author: Codex, at Albert Wong's request to make a recommendation.
Baseline: `6d7ac409d8c9f11163b4f4cec63e2d4da00e69f2`.
Status: design proposal; **no-go for production integration**. This is an
engineering recommendation, not a Redis server-owner feasibility approval.
No new server command, module, storage format or deployment support is claimed.
Disposition: retain the stock-server path and end this client integration
investigation with a no-go result. A future server implementation is a separate
decision requiring server-owner feasibility/contract approval before integration.

## Decision and alternatives

Keep the existing stock-server Search/HMGET/Arrow path. Pursue a native, per-shard
typed export only if server owners can commit to the semantic contract below
and name supported Redis Software/Cloud versions. Do not make the independent
client overlap experiment depend on that capability.

| Approach | Costs it can remove | Costs/dependencies that remain | Recommendation |
|---|---|---|---|
| Native typed shard export | Repeated HMGET framing, string reply objects, client string parsing and Arrow assembly; compression might reduce payload bytes | Server HASH reads, typed conversion, discovery, index/DDL checks; new server capability and service support | Preferred research direction; no production integration until feasibility and elapsed gates pass |
| Lua/Function wrapper | Repeated field names and request framing | String replies, client typed decode/Arrow assembly, bounded blocking server work, scripting ACLs, declared keys and same-slot restriction | Reject as the typed-export implementation |
| Prebuilt column blocks | Per-read HASH reconstruction and conversion for already-built blocks | New authoritative storage or a maintained materialization, write amplification, update/delete invalidation, defaults/schema migration | Separate storage project; cannot transparently replace authoritative HASH reads |
| External Flight service wrapping this driver | Downstream transport framing; placement may shorten the RESP link; connection reuse | All upstream Search/HMGET/string conversion work, another service, auth and resource ownership | Useful transport product, insufficient evidence for this issue's transfer improvement |

Historical [matched evidence](../benchmarks/wide-transfer/cross-database-2026-10-22/README.md)
measured approximately 346 MB Redis versus 111 MB PostgreSQL COPY and 79 MB
ClickHouse ArrowStream for a one-reader wide transfer. Its Lua screen cut
requests from roughly 193 MB to 3 MB but regressed regular/partition elapsed
time by 25.7%/23.5%. Those measurements used `aab9612`, not this baseline, and
do not establish a speedup for any proposed API. Sending field names once is
insufficient grounds to promote a wrapper.

## Proposed version 1 contract

Names here describe logical operations, **not registered Redis commands**.
Implementing a fake command in the driver would not establish server feasibility.

`Capabilities` returns an explicit export major/minor version, supported Arrow
physical/logical types and encoding/compression, deployment version, shard
routing model, maximum page rows/bytes, maximum active cursors and cursor memory
budget. An unknown command means unavailable. ACL denial means unavailable to
this principal, not proof that the command is absent. Neither result enables
the feature. Capabilities are tied to server identity and invalidated on
reconnect/failover. Never infer support from a product name or install a module
or Function automatically. RESP3 remains required by the current driver; this
proposal does not restore the historical issue's RESP2 scope.

`Open` accepts version, table generation and schema fingerprint, immutable
projection field IDs in output order (including duplicates/aliases), physical
types, a validated shard-local discovery predicate/range, row and byte budgets,
deadline and a client execution ID. Server lookup must validate these against
authoritative metadata; it must not accept arbitrary type/default code.
The response supplies the accepted fingerprint, shard/routing epoch, opaque
cursor token, page sequence, row count, end flag and one Arrow IPC stream.
Each page is independently decodable with a schema; optimizations to send a
schema once require a separately negotiated minor version and reconnect rules.

Type rules:

- Signed/unsigned integers use the declared Arrow width, range-checked before
  emission. Preserve values above 2^53; JSON numbers and Search numeric values
  cannot act as authoritative integer values. Overflow is an error.
- Booleans use explicit stored-value decoding. Floats preserve the driver's
  NaN/infinity and signed-zero semantics; no numeric/string coercion shortcut.
- DATE uses epoch-day date32. TIMESTAMP uses signed epoch-microsecond timestamp
  with the declared timezone semantics; timezone-free timestamps must not gain
  a timezone. TIME/interval follow the driver's physical Arrow schema. Reject
  unsupported precisions/types explicitly instead of narrowing them.
- DECIMAL uses the declared precision/scale and exact decimal128/256 integer
  coefficient with checked rescaling. Never round through binary floating point.
- Strings use Arrow offsets and length-delimited bytes, preserving embedded NUL
  and empty strings. Follow the existing driver's string/UTF-8 validation rules.
  Binary has a separate logical type; never base64 or terminate on NUL. CHAR
  padding, VARCHAR lengths, arrays and other supported driver types must match
  existing semantics or make the entire query ineligible for export v1.
- An existence bitmap/internal row ID distinguishes vanished rows from existing
  rows whose projected columns are all NULL. Vanished rows are omitted;
  all-NULL rows retain multiplicity. Validity bitmaps distinguish NULL from
  zero/empty. Decode and validate a present internal row ID even if unselected.
- Missing fields use immutable column default metadata, its `MissingThrough`
  epoch and explicit NULL markers exactly as the driver does. A stored NULL
  must not become a default. Added columns and altered defaults invalidate the
  schema fingerprint; no cursor crosses a schema or row-prefix generation.
  Unsupported defaults/types trigger query-level fallback before any output.

## Ordering, visibility and limits

Discovery owns key membership and order; authoritative HASH reads supply values
at fetch time. Concurrent updates may appear in later pages and deletes between
discovery/fetch disappear. There is no snapshot, transaction or new consistency
feature. Preserve the driver's generation/readiness error checks before and
after discovery and at end-of-read. Do not mask a dropped/rekeyed table as EOF.

Return stable discovery order within each shard. The coordinator preserves
existing public partition semantics (no global order) and merges only when it
can reproduce an ordinary query's promised order. Global LIMIT/OFFSET and
residual predicates stay at the coordinator unless safe pushdown is proved.
Tie sorting, joins, aggregates, nontrivial expressions and unsupported layouts
retain the existing query path. Do not apply a global limit independently per
shard. Preserve duplicate rows and duplicate output columns.

## Bounds and lifecycle

Negotiate both row and serialized-byte caps; independently cap server scratch,
Arrow buffers, schema size, maximum cell size and total active export memory.
A row/cell that cannot fit a page returns an explicit size error before that
page is emitted. No silent truncation or unbounded single-row exception.
Encoding must account for buffers, offsets, validity and compression workspace,
not only the final compressed payload. Admission failure is a resource error.

One outstanding page per cursor; the server performs no speculative work until
the next pull. A client may retain at most its current page plus one lookahead
page. Backpressure therefore bounds page count; the negotiated byte cap bounds
each page. Cursor metadata and discovery/sort state need separate quotas and
an explicit server-owner memory estimate. Arrow buffers belong to the returned
record until its final release, independently of cursor close/reuse.

`Read` accepts the cursor token and next sequence. EOF is explicit; a malformed,
oversized or partially delivered page is an error. Propagate server type,
generation, ACL, routing and resource failures through ADBC without returning a
successful partial result. Idle cursors expire after a negotiated bounded TTL;
expiry is an error rather than silent EOF. `Close` is idempotent and releases
discovery/output state. Cancellation stops I/O, joins fetch/conversion work,
then closes cursors/connections and releases worker tokens. Server-side work
must poll its cancellation/deadline at bounded intervals; stopping client I/O
alone does not prove server cancellation.

Do not replay stateful Open/Read after an ambiguous lost response. Version 1
has no durable replay cache; fail the stream and let the caller restart the
whole query. Cursor close may be retried. Retrying before any server execution
or returned data is permitted only with a proved idempotent request identity.
Never silently fall back after emitting pages: the two executions could observe
different data and duplicate rows. Fall back during planning/capability checks.

## Cluster and deployment boundary

Each cursor belongs to its original shard/coordinator identity and routing
epoch. Open only on a verified owner. Individual HASHes can occupy arbitrary
slots; group discovery results by verified owner and issue shard-local reads.
Do not use a same-slot script on arbitrary row keys, nor assume that an
Enterprise proxy endpoint implies a same-slot Cluster command. A server-owner
contract must describe safe shard-local access and ownership validation.

MOVED/ASK before cursor creation may cause bounded rediscovery against the new
owner. Once Open may have executed, ambiguous redirections/failover or a changed
epoch invalidate that cursor. Stop and report a routing error; do not resume on
a different shard or delete an unrelated cursor with a coincidentally equal ID.
Do not claim a cross-shard snapshot. The coordinator needs per-reader budgets,
bounded merge state and joined cleanup for every shard.

Require the existing verified TLS/authentication configuration and a separately
documented export ACL category. Capability checks must not broaden credentials.
Server export must enforce read access to the table/index and every underlying
key; projecting a field cannot bypass key ACLs. Restrict cursor tokens to their
principal/session, bound token input and avoid logging tokens or data values.
Redis Software/Cloud service owners must explicitly approve command/module
availability, upgrade behavior and resource accounting. Stock-server fallback
remains mandatory. No support is asserted for a deployment without that decision.

## Feasibility and measurement exit gates

Before implementing production integration, obtain a named server-owner decision
covering command availability, managed-service rollout, shard access/migration,
typed/default semantics, memory accounting and interruptible bounded execution.
No such decision was supplied in this session. Recommendation: **no-go for
production integration now; retain the contract for feasibility review**. A
client-only mock or Arrow codec roundtrip cannot substitute for that decision.
No production API or storage change is selected here. The issue explicitly
permits a no-go result when capability/semantic requirements cannot be met; this
is that disposition, not a claim that the future native API is implemented or
has server-owner approval. The feasible-prototype performance gate below remains
mandatory if that disposition is revisited.

### Observed stock-server diagnostic

The [read-only probe](../benchmarks/typed-export/probe.py) ran on the dedicated
verified 4x QPF Redis Software fixture (Redis 8.6.2, Search 8.6.11, authenticated
verified TLS and RESP3). [Raw sanitized output](../benchmarks/typed-export/2026-11-03/probe.json)
records installed module versions, no command names matching `*arrow*`, and a
concrete exactness failure: `EVAL_RO` returned the original string
`9007199254740993` intact but `tonumber(ARGV[1])` returned `9007199254740992`.
The script touched no keys and did not depend on table contents or key slots.

Name matching is diagnostic only: it does not prove the absence of every possible
server facility. The number test rejects **naive** Lua numeric conversion, not
all conceivable exact software encoders. Returning strings avoids that failure
but retains client parsing, and a same-slot wrapper does not satisfy the required
arbitrary-key per-shard ownership contract. No implementation of the proposed
negotiated typed-export contract is available in this work. The supported
[FT.AGGREGATE API](https://redis.io/docs/latest/commands/ft.aggregate/) returns
row maps/arrays, not this Arrow export contract.

These observations, the historical wrapper elapsed regression, and the absence
of an approved native server capability support the engineering **no-go now**.
They do not establish that a new native export is impossible. No module is
installed, managed-service capability is assumed, or lower-byte result is
represented as a successful native server prototype.

If feasible, implement a separately deployed prototype, preserve its raw failures
and compare with the optimized driver at the same source revision. Restore a
verified compatible backup on Redis Software with active verified 4x QPF. Use
independently typed 100K × 105 and three-column fixtures, regular/public partition
APIs, one/four readers, one warm-up and at least five alternating measured
trials. Fully fetch each result and independently compare every selected value
and duplicate multiplicity outside timing. Match batch rows/bytes, RESP3,
compression and TLS; record actual first-batch size. Report elapsed ranges,
client/server CPU, cumulative allocations, retained/peak memory interpretation
and request/reply bytes in both directions. Repeat with controlled resources.
Lower bytes or CPU alone do not pass the elapsed gate. Record regressions and
capability failures; do not promote based on synthetic codec throughput.

Protocol constraints referenced in the earlier assessment:
[declared script keys](https://redis.io/docs/latest/develop/programmability/lua-api/),
[blocking script execution](https://redis.io/docs/latest/develop/programmability/eval-intro/).
