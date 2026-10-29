# Verification: dlt integration

Date: 2026-10-29. Base source: `95d6583`; evidence includes this working diff.
Author: Codex. [Intent](intent.md), [specification](spec.md), [plan](plan.md).

## Environment

Local macOS arm64, Go 1.27.2, Python 3.12, dlt 1.31.0, PyArrow 26.0.0,
ADBC manager 1.13.0. Built the current checkout's native library with
`GOCACHE=/tmp/redis-dlt-go-cache make -C go build`. Used the existing local
Redis Software test container (`redislabs/redis:8.2.0-78.18`, port 16384), with
unique per-test SQL schemas removed on fixture teardown. No compatibility claim
for untested servers, architectures or Python versions follows from this run.

Dependencies were installed in `/tmp/redis-dlt-tests`; dlt's local state was
redirected to `/tmp/redis-dlt-state` and telemetry disabled.

## Executed checks

```sh
DLT_DATA_DIR=/tmp/redis-dlt-state RUNTIME__DLTHUB_TELEMETRY=false \
  REDIS_URI=redis://localhost:16384/0 \
  REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.dylib" \
  /tmp/redis-dlt-tests/bin/python -m pytest -q python/tests/
```

Result: **28 passed, 2 skipped in 12.99s**, including all **12 dlt tests**.
The skips are the existing opt-in DuckDB community extension download and
Superset smoke test requiring an isolated Superset environment.

dlt tests fully fetch and compare rows after multi-batch ingestion and a later
append, decimal/date/UTC timestamp/binary/boolean/float/text/NULL round trips,
Arrow input, multiple tables, nested JSON, URI secret configuration and native
options. They exercise quoted schema names, omitted internal metadata,
incremental checkpoints after reopening the pipeline, terminal replace/merge
rejection before connecting, and recovery after an injected transient failure
before the second batch. The recovery test observes the first batch alone after
failure, then exactly the five expected rows after resuming. It does not claim
atomicity for a failure during a native write.

The runnable `python/examples/dlt_pipeline.py` was executed against the same
server after checking its example schema was absent. A separate driver query
verified `[(1, 10.5), (2, 20.0)]`; the example schema was then removed.

`pip wheel --no-deps --no-build-isolation ./python` succeeded. Inspecting that
development wheel confirmed `adbc_driver_redis/dlt.py` and the `dlt` extra in
package metadata. This is a Python packaging check, not a published native
release wheel. `pip check` reported no broken dependencies. The modified CI
YAML parsed successfully, and `git diff --check` passed.

## Review and limits

Reviewed lifecycle, identifier quoting, secret configuration, append semantics,
partial-write/replay risks and optional dependency boundaries against REVIEW.md.
No actionable finding remains from this local review. This is agent review,
not independent human approval.

CI now runs dlt tests on Redis Software single-database runners for Linux amd64
and arm64. Existing OSS Python analytics jobs also include dlt. Those modified
CI jobs were not executed locally. No native driver behavior changed, so the Go
suite and unrelated optional smoke tests were not rerun. No release was made.

## PR preparation after rebase

At the user's request, rebased onto repository main `8d2cf8f`. Preserved its
Python 3.12 minimum, refreshed dependency floors, Dask integration, shortened
README and current CI coverage matrix. The dlt extra now also requires
`pyarrow>=26.0.0`, matching the current driver integrations.

Rebuilt the native library from the rebased checkout. Before testing, verified
the existing Redis Software fixture's effective configuration through its local
admin API: active database, active QPF scaling factor 4, six Search workers,
`mnp` routing and 32 connections. No configuration change was required.

Reran the command above selecting `python/tests/test_dlt.py` and
`python/tests/test_driver.py`: **16 passed, zero skipped in 13.32s**, including
all 12 dlt tests. The earlier full-suite and wheel checks above describe the
original implementation base, not a full-suite run of the newer main branch.
Parsed the resolved TOML and CI YAML and checked the diff for whitespace and
unresolved conflict markers. No post-rebase full-suite or hosted CI result is
claimed.

## Correct repository preparation

The replacement PR targets `redis/redis-adbc-driver`, based on main `71625e1`.
Rebuilt its native library successfully and preserved its current dependencies
and integrations while resolving the patch. Earlier live-test results above
remain historical evidence from their stated bases. The local Redis Software
fixture was stopped before this preparation, so no live Redis result is claimed
for the public repository base. Hosted CI will exercise the current branch.

Public-base checks: dlt tests without Redis reported **5 passed, 7 skipped**
(the seven live integration cases require REDIS_URI). TOML/CI YAML parsed and
`git diff --check` passed.
