# Independent catalog writes: validation

Date: 2026-11-16. Source baseline: `225e0056` plus this working diff.
Status: local verification complete; no release or human approval claimed.
See [intent](intent.md), [specification](spec.md) and [plan](plan.md).

- Baseline regression: `TestSnapshotConcurrentCatalogStatements` fails with
  three `40001` errors among four independent CREATE SCHEMA statements.
- Candidate: the same test passes schema creation, three rounds of four CTAS,
  views, renames/drops, exact view fetches and final catalog absence checks.
- Runtime: dedicated local Redis Software Redis 8.8.0, three primary shards,
  authenticated TLS; active QPF 4, six Search workers, mnp, 32 connections verified
  by `make -C go redis-up`. macOS arm64 client through a Docker published port.
  No performance claim. Operational details and logs remain outside git.

## Intermediate candidate (not the final implementation)

Serializing only catalog writers passed the initial native and Arrow checks but
was insufficient: the native dbt adapter at `b9584f1`, with its `_publish` SQL
retry wrapper disabled, reported 189 passed, 4 failed, 19 skipped and 1 error
under `uv run pytest -n 4 -q` (four dbt model threads). All five failure/error
reports contained `40001` publication conflicts in snapshot/microbatch paths.
DML can read catalog membership metadata; catalog admission must also exclude
admitted table writes. These results are retained as intermediate evidence and
must not be described as final validation.

The final candidate makes catalog admission exclusive with both catalog and
admitted table writers. Table admission rejects an admitted catalog owner.
Unadmitted pins do not block one another. Temporary-object cleanup participates
in catalog admission. Disjoint admitted table writers still run concurrently.
The native regression `TestSnapshotCatalogDMLMetadataDependencies` stages one
DML catalog read, blocks unrelated DDL, publishes the original DML once, and
then allows DDL from a fresh root.

## Final candidate

- Unit-only suite: `cd go && env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test
  -count=1 -p 1 ./...` passed (main package 28.019 s). Redis-dependent
  skips in this command do not establish server compatibility.
- Software safety selection: passed (34.211 s): catalog concurrency/admission,
  independent publication, table/Arrow admission, same-resource conflicts,
  expired writer fencing, lost publication replies, Arrow ingestion, DDL/views,
  temporary-object handling and unmanaged Arrow tests.
- Native library: `make -C go build` passed with the Makefile version linker flag.
- Final focused catalog regressions passed (3.058 s), including exclusive
  catalog/table admission, lease renewal/expiry, read-only/stale pin refusals,
  no consumption on canceled Arrow creation/append, and the DML catalog-read case.
- The issue's two-connection native Python reproduction completed all 40 CTAS
  statements with zero failures. Every result was fully fetched and checked;
  all 40 table OIDs were distinct. GetInfo reported the source build version
  `v0.1.0`, not a released fix.
- Final native dbt rerun: **194 passed, 19 skipped, zero failures/errors**,
  135.86 s. Four pytest workers, four dbt model threads, and adapter SQL
  publication retries disabled. The complete multi-model concurrency, snapshot
  and microbatch cases passed. Skips retain the adapter suite's capability/
  environment exclusions; they are not passing coverage.

Frozen final macOS arm64 native library SHA-256:
`22b0de54e177138df6185433879139c121e6ca0d4389868585bde686598f5336`.

## Native dbt reproduction method

The exported adapter runtime, tests and dependency definitions match
`redis/redis-dbt-adapter@b9584f1` (43 file blobs independently checked). Tests
use Python 3.13.16, dbt-core 1.12.6, dbt-adapters 1.24.5,
dbt-tests-adapter 1.20.0, adbc-driver-manager 1.12.0, PyArrow 26.0.0,
pytest 9.1.1 and pytest-xdist 3.8.0. The fixture sets `threads: 4`.
The existing `TestConcurrency` exercises multiple independent table/view models,
dependent models and explicit expected SQL failures through the native driver.

Set authenticated scratch Software connection/trust variables and
`REDIS_ADBC_DRIVER` to the built native library. Export the adapter source to an
isolated directory and use its `v1` uv project. Load this local pytest plugin as
`issue448_no_replay` on `PYTHONPATH`:

```python
def pytest_configure(config):
    from dbt.adapters.redis_adbc.connections import RedisAdbcCursor
    RedisAdbcCursor._publish = lambda self, action, replayable: action()
```

Run `uv run pytest -p issue448_no_replay -n 4 -q`. The plugin executes each
publication action once, disabling the adapter's own conflict/ingestion retry
wrapper in both controller and worker processes. This tests the driver fix
without changing the adapter source or hiding native conflicts.

## Review and limits

Codex self-review covered the three `REVIEW.md` passes: leased admission and
cancellation/Close cleanup, same-resource/read-source conflicts and unknown
replies; immutable storage and external ownership; scope, tradeoffs and evidence.
No actionable finding remains from that review. This is not human approval.

The implementation coordinates before SQL/default evaluation or Arrow input,
retains server-side fences and publication outcome resolution, and adds no SQL
replay. Disjoint table writers remain concurrent. Catalog writers are exclusive
with admitted managed table writers; long CTAS/index work can delay them and
there is no fairness guarantee. Mixed driver versions and direct import/
registration APIs retain their documented conflict limitations.

The full Go integration matrix, platform matrix and benchmarks were not rerun.
The complete unit pass and targeted live Software safety checks are listed
above. Local Markdown links and `git diff --check` passed. The dedicated Software
fixture was rechecked for effective 4x QPF after acceptance and removed after
validation. Private logs, endpoints, certificates and runtime artifacts are not
part of the change.
