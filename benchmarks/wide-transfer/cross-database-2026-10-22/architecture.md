# Where a larger transfer improvement would come from

Assessment dated 2026-10-22 for [#442](https://github.com/alberttwong/redis-adbc-driver/issues/442).
These are design constraints and hypotheses, not measured speedups.

## Current client-only boundary

HASHes remain authoritative and Search discovers actual keys. Independent HMGET
commands route through go-redis to the owning Cluster shards. The driver then
decodes string values and applies stored-value, default/NULL and output-type
semantics before appending Arrow columns. ADBC exports the resulting Arrow
buffers, but does not make the upstream RESP representation columnar.

Preboxing immutable command field names reduces client work without reducing
command count or wire bytes. The retained command-arena screen did not improve
elapsed time, so production code remains unchanged. Prebound specialized Arrow
appenders could reduce per-cell dispatch, but must retain integer range checks,
timestamp precision, CHAR padding, null/default rules, error ordering and
unselected-required-field validation. Bypassing Coerce merely because the stored
type equals the output type is unsafe: stored integer decoding does not itself
enforce every destination-width constraint. This candidate needs its own
differential tests and repeated measurements before promotion.

Bounded discovery/fetch/conversion overlap is another client candidate. It must
keep replies alive until consumption, serialize cursor READ/DEL, preserve output
order and cursor generation/readiness checks, stop both stages on cancellation,
and join background work before releasing connections/worker tokens. Prefetch
also moves the time at which concurrent HASH changes are observed. It should be
screened separately from command allocation, with a one-page bound and explicit
concurrent-update tests. No overlap speedup is asserted here.

## Bulk server export options

| Option | Possible benefit | Required capability and constraint |
|---|---|---|
| Redis Function/Lua wrapping a page of HMGETs | Send field names once per page; potentially less request framing | Additional EVAL/FCALL and script/function ACLs; bounded execution blocks each shard while running; all accessed keys must be declared and Cluster multi-key calls must share a slot. Existing arbitrary row-key slots cannot be combined into one cross-slot script. Returned strings still require client typed decoding and Arrow construction. |
| Server module exporting typed columnar batches | Avoid RESP objects and much client conversion; encode schema once; potentially compress buffers | New Redis server/module capability, protocol and version negotiation, explicit support by each managed service, per-shard ownership and coordinator behavior. Stock-server support must remain available. |
| Change storage to prebuilt Arrow/column blocks | Scan ready-made buffers with fewer conversions | Changes authoritative storage, update/read consistency, schema evolution, missing defaults and index synchronization. Requires a migration and a separate storage design; does not transparently accelerate existing HASH tables. |
| Flight service around the existing driver | Reuse Arrow batches for remote consumers | Existing Search/HMGET/decode work remains upstream. Benefits depend on execution placement, connection reuse and compression; Flight alone does not remove the measured work. |

A server export contract must specify projection and type/schema version,
existence versus all-NULL rows, missing fields/default epochs, binary-safe strings,
exact integers, row/order/limit semantics, cancellation and cursor expiry,
bounded memory/output, retry behavior, Cluster migration/redirection and ACL/TLS
requirements. Existing two-stage reads do not provide snapshot isolation; a new
API must state its consistency model explicitly rather than imply one.

Redis documents both the [declared-key/Cluster constraints](https://redis.io/docs/latest/develop/programmability/lua-api/)
and [atomic execution blocking other server work](https://redis.io/docs/latest/develop/programmability/eval-intro/).

## Decision

Keep the current stock Redis path and do not introduce a server dependency from
this benchmark. Use the matched results to quantify the opportunity. The next
client work should isolate typed appending and bounded overlap, each with exact
semantic tests and a repeated end-to-end speed gate. In parallel, discuss a
versioned, bounded per-shard typed export with Redis server owners; confirm
managed-service support and Cluster coordination before prototyping a module.
No server implementation or service compatibility is established by this report.
