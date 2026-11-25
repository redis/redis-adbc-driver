# Intent

Requested 2026-11-24: fix [#641](https://github.com/redis/redis-adbc-driver/issues/641). Base: `12f46745` (main). Status: local fix; see [spec](spec.md), [plan](plan.md), [validation](validation.md).

In the TPC-H SF1 Flex three-way comparison, Q11 through DuckDB over the Redis ADBC catalog completed all four runs. Its validation then failed with only `invalid load key, '\x00'.`, a pickle decode error from reading a private result file back. The harness gave no file, repeat, size or offset. It then deleted every result file, so nothing could be inspected. The same query directory's reference had just validated the direct-driver result, so the reference was intact.

Affected consumers: anyone running `benchmarks/olap-release` and reading its `queries.jsonl` or rendered report, in particular across Docker bind mounts where the engine worker and the validator are separate processes.

Desired:
- A result file that does not read back exactly as its worker wrote it is detected even when it still decodes. It is reported as a failed validation with its location, never as a result mismatch.
- The files of any failed validation stay in the private profile for inspection.
- The worker's writes are durable before the validator reads them.

Excluded: the root cause of the Q11 zero bytes, which remains unknown (host bind mount, another writer in the worker process, or something else). Also excluded: automatic re-reads or retries that could turn a failed exchange into a pass, changing where artifacts are stored, and changes to query execution, timing or the validation tolerance.
