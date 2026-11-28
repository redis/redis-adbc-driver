# Intent

Requested 2026-11-27 by the session fixing TPC-DS SF1 failures on Redis Software Flex ([#727](https://github.com/redis/redis-adbc-driver/pull/727), [#729](https://github.com/redis/redis-adbc-driver/pull/729), [#730](https://github.com/redis/redis-adbc-driver/pull/730)). Base: main `e57172a7`. Status: local fix with tests; see [spec](spec.md), [plan](plan.md), [validation](validation.md). Follows [olap-artifact-integrity](../olap-artifact-integrity/intent.md) ([#641](https://github.com/redis/redis-adbc-driver/issues/641)).

Focused runs of `benchmarks/olap-release` twice lost a query before Redis could run it. The native DuckDB reference failed the validator's own artifact check, so the record was `duckdb_status: validation_failed` and every Redis-backed engine was `not_run_no_reference`:

| Run | `duckdb_artifact_problem` |
|---|---|
| TPC-DS SF1 Q72 | `role: "validator scratch"`, `file: "sort-0"`, 0 objects decoded, no decode offset; recorded 20,839 bytes `ffc9fc79…`; read back 20,839 bytes `468bbb87…`, first all-zero 4 KiB page at byte 0 |
| TPC-DS SF 0.1 Q78 | same role, file and object count, no decode offset; recorded and read back 20,280 bytes, both `b56b0b6b…` |

Both ran on a macOS Docker Desktop host, with the private profile (where the per-query mode-0700 directory lives) bind-mounted at `/private` into the Linux client container `adbc-olap-release-client:20261123`. Other containers ran on the host at the same time. Q72 also blocked a Redis diagnostic; Q78 broke a same-fixture A/B comparison.

Affected consumers: anyone running the OLAP harness with its private profile on a Docker Desktop bind mount, and anyone reading `<engine>_artifact_problem` to tell what went wrong.

Desired:
- The validator's own sort/merge scratch never depends on the bind mount.
- An artifact problem says what the decoder itself read, separately from the later re-read the error path makes, so a record like Q78's (matching digests, yet a failure) explains itself.
- A real difference is never masked: no retry, no acceptance of bytes the decoder did not see.

This supersedes the earlier record's exclusion of "changing where artifacts are stored" for the validator's private scratch only. The engines' result files stay in the private profile, and retries stay excluded.

Excluded: moving the cross-process engine result files, the Docker Desktop file-sharing defect itself, query execution, timing and the validation tolerance. No Redis server is involved in this change.
