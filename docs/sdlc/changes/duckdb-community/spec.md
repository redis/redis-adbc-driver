# Contract

Date: 2026-10-20. Author: Codex. Status: implemented; review pending.
See [intent](intent.md), [plan](plan.md), and [evidence](validation.md).

`redis_adbc_scan(uri, sql)` accepts non-NULL redis:// or rediss:// URI strings
and exactly one SELECT, validated with the Redis driver's own parser. It
describes the result during binding without executing SQL, then opens a new
connection at execution and validates column names and physical Arrow types.
Bind-time and execution-time handles have independent, bounded lifetimes.
Prepared statements execute repeatedly. Reads are not snapshot-isolated across
binding and execution.

The extension embeds the Go driver as a C archive and uses DuckDB's Arrow
conversion. There is no driver-manager or separate library dependency at
runtime. The build pins Go 1.26.6 with official SHA-256 checksums. DuckDB and its
extension CI tools are pinned submodules. Native Linux and macOS are targets;
Windows, Wasm and musl targets are excluded. macOS requires 12+.

The first version has no ATTACH, writes, ingestion, parameter binding, or
DuckDB filter/projection pushdown. Queries supplied to the scanner can use
the driver's Search pushdown. Schema changes between binding and execution
fail. Streaming errors and early close release Arrow and ADBC resources.
DuckDB's external-access setting is enforced before connecting.

Acceptance: compile and load a distributable extension; verify complete Arrow
fetches, multiple batches, NULL/Unicode/decimal/date values, outer projections,
filters, joins, empty scans, LIMIT, repeated execution, stream errors, schema
changes and argument/query rejection. Run offline SQLLogicTests and validate
the descriptor with the community repository's own preparation script.
