# Specification

See [intent](intent.md), [plan](plan.md).

Private connection.json field duckdb_adbc_scan_threads defaults to integer 1.
Only exact integers 1..1024 are accepted; booleans, strings, floats, NULL and
other shapes fail before the third-path DuckDB connection opens. Native DuckDB
and direct Redis retain their established controls.

For value 1, omit scan_threads from ATTACH entirely. For a larger value, insert
only its validated numeric literal alongside the existing READ_ONLY and
query_pushdown false options. Driver db_kwargs, including independently selected
adbc.redis.partition_count, retain existing forwarding and forced timeout/telemetry
rules. Unsupported extension options propagate failure and close the connection;
never silently retry or substitute serial execution.

Third-path provenance adds numeric requested_scan_threads and boolean
custom_scanner_global_symbols. The thread count records the
request, not actual readers, partitions, concurrency, or effective CPU limits.
Existing sanitized provenance remains; profile/credentials, driver options and
paths are not exposed. Custom scanners must match the installed DuckDB ABI and
support the opt-in option. Actual parallel execution needs independent evidence.


Private duckdb_adbc_global_symbols defaults false and accepts only JSON booleans.
True requires ADBC_SCANNER_EXTENSION before DuckDB opens. Only the third-path
branch may promote the already imported _duckdb module with ctypes.RTLD_GLOBAL,
before loading that custom extension. Neither a configurable library path nor a
different runtime is loaded through this control. Native DuckDB/direct Redis and
the default released-scanner path do not perform promotion. Existing ABI checks
still apply when the extension loads; this control does not relax them.
