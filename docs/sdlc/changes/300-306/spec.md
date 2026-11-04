# Contracts

See [intent](intent.md) and [plan](plan.md). Implementation contract, 2026-11-04.

## #306: sorting

Private query working slices check cancellation before sorting, every 256
comparisons and after sorting. A private per-invocation panic token unwinds the
standard library sort; only that token is recovered. Comparator errors/panics
are never swallowed and comparisons never return fabricated ordering answers.
Callers return the cancellation error before emitting or publishing results.
Cancelled/Timeout and SQLSTATE 57014 use the existing context mapping.

Inventory: scanned projections, set operations, tie refinement, windows,
aggregate ordering/ordered sets, array/string/JSON aggregates, expanded rows,
top-K finalization, spill runs, retained capture and recursive SEARCH ranking.
Metadata/catalog/schema ordering and per-value JSONB canonicalization are outside
this row-cardinality sorting contract. The interruption bound counts comparisons,
not wall time: a single large-key comparison or swap may take additional time.

## #300: bounded DISTINCT

Only explicit `stream_required=true` admits the new path: unordered row-local
managed-table DISTINCT, with resolved output types and no DISTINCT ON, subquery
or correlated state. Map/Union-containing results retain rejection in this
selected path. Skip the DISTINCT-to-GROUP-BY optimizer in this mode.
Cap source allocated IDs/prepared keys and projected rows at two million;
cap retained unique keys at 65,536 entries and 16 MiB charged bytes including
map overhead. Reject oversized rows before key serialization and exact oversize
keys before admission. No spill or retry into materialization. Errors may arrive
after earlier batches; cancellation/release clears the state and cursor.
OFFSET/LIMIT follow deduplication. Without ORDER BY both SQL paths have undefined
order, and ordinary grouped DISTINCT can choose different valid LIMIT rows.

## #301: dictionary IPC files

A private 0600 IPC stream stages finite batches; a second pass remaps their
indices to file-wide int32 String/Binary dictionaries. Limits: 65,536 entries/
16 MiB payload per column; 262,144 entries/64 MiB payload across columns; 4,096
fields, 16,777,216 cells, 65,536 batches and 256 MiB staging bytes per file. Dictionary entry visits also
cap at 16,777,216 and cumulative visited dictionary payload at 256 MiB, including
unused entries and repeated dictionaries; remapping visits each entry once per
batch before translating row indices.
All failure/cancellation paths delete staging and partial output. No plain-array
fallback. Consumer records own their references after reader release.

## #302: partition layouts

Live revision 3 and retained revision 4 carry bounded non-dictionary layout
maps; canonical revisions 1/2 retain their wire meaning. Readers reject malformed
revisions/layouts and independently validate source-schema compatibility.
Retained chunks keep canonical IPC validation and schema digests. Producers
validate all conversions before publication; replay applies those conversions
again after source/producer lifetime ends. Dictionary partitions remain rejected.
All selected fixed-width binary columns together must fit within 16 MiB per row.

## #303: NULL statistics

The authoritative request returns standard key 5 per physical column, int64
union values, alongside key 6 table row counts. Search requests keep row counts
only. Both are marked approximate because concurrent DML is not isolated.
Empty tables return zero for known columns. Empty values and NaN are non-NULL;
absent fields use stored defaults/explicit NULL markers. Other quantities omit.
HEXISTS avoids fetching arbitrary payloads. One-minute deadline, 64 MiB identity
budget, two million key/field-probe work units, at most 4,096 columns; explicit
exhaustion, permission and identity-change errors. No source snapshot.

## #304: byte-length functions

OCTET_LENGTH/BIT_LENGTH share the ordinary evaluator and partition allowlists.
One VARCHAR/VARBINARY argument, BIGINT output; NULL propagates, empty means zero,
Unicode counts UTF-8 bytes and embedded NUL counts. Other types (including fixed
CHAR), bad arity and unsafe query shapes reject. Header-only length inspection
adds no allocation or output expansion. Retained global evaluation stays once
before chunking under its existing resource quotas.

## #305: nested extension normalization

Registry revision 2 extends only UUID/JSON storage normalization through STRUCT
fields. Reject remains the default. Unknown names, invalid metadata, mismatched
storage and unapproved nested containers reject. Bound every nested schema to
16 levels, 4,096 fields and 64 KiB aggregate metadata before recursive mapping.
Converted batches use the existing million-value/64 MiB decoded-payload budget.
Buffers retain offsets, null bitmaps and ownership; JSON under NULL containers
is hidden. Canonical storage schemas and values cross Bind/BindStream,
create/append/reload, IPC and retained replay; extension identity is discarded.
