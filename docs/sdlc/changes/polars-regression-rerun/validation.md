# Polars regression rerun validation

Date: 2026-10-29. Author/reviewer: Codex. Human review pending.
See [intent](intent.md), [spec](spec.md), and [plan](plan.md).

## Results and provenance

Both runs used macOS arm64, Python 3.12.14, Polars 2.1.0, PyArrow 26.0.0,
ADBC driver manager 1.13.0 and pytest 9.2.0. Each native library was freshly
built with `GOCACHE` pointing to a writable temporary cache and
`make -C go build`, preserving the Makefile's version linker flag.

| Run | Driver source | Server | Result |
|---|---|---|---|
| Initial requested rerun | `95d6583` | Disposable Redis Community 8.6.2 with Search | 8 passed in 0.14s, zero skips, warnings treated as errors |
| PR validation | `da1e7c1561938588fd028a31d17c5117bf845b82` (upstream main) | Disposable Redis Software, Redis 8.6.2, one RAM shard, Search, verified active 4x QPF | 8 passed in 0.66s, zero skips, warnings treated as errors |

The initial checkout did not contain `test_polars.py`, so that run extracted it
unchanged from `7889ec34883b705aedffd22e0fdd937140218614` into a temporary
directory alongside the checkout's `conftest.py`. The PR validation used the
existing upstream test file, which is identical to the linked revision.
Neither run changed driver or test behavior. An initial sandboxed attempt
could not access the local TCP endpoint; the completed runs had local network
access. The initial Community run is compatibility evidence, not Software or
QPF evidence.

Before the Software run, `make -C go redis-up` successfully verified active
QPF with scaling factor 4, six effective Search workers, `mnp` routing and 32
connections via the fixture's management API checks. It also checked the
one-shard RAM topology and Search readiness.

## Executed cases

All eight cases passed in each completed run:

- `test_polars_roundtrip[connection]`
- `test_polars_roundtrip[uri]`
- `test_polars_append_filter_and_aggregate`
- `test_polars_empty_result_preserves_schema`
- `test_dbapi_and_polars`
- `test_autocommit_rejected`
- `test_manifest_discovery`
- `test_collation`

The Polars fixture writes int64, strings, float64, booleans, dates and
microsecond timestamps, including NULLs. Reads must match exact Polars types
and values. Append verifies the row count; filtering and grouped sums are
compared with independently computed Polars results. Empty reads must retain
the complete schema. Test tables are dropped during cleanup.

## Reproduction

In a Python 3.12 environment with the dependency versions above and the local
Python package available, run from the repository root:

```sh
make -C go redis-up
make -C go build
PYTHONPATH=python \
REDIS_URI=redis://localhost:6379/0 \
REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.dylib" \
python -m pytest -v -W error \
  python/tests/test_polars.py python/tests/test_driver.py \
  --junitxml=/tmp/polars-regression-results.xml
```

Use a dedicated disposable Redis Software fixture and the URI emitted by its
setup script when overriding ports. On Linux the library suffix is `.so`;
Linux was not exercised in these local runs. The Software fixture created for
this PR was removed after validation.

## Review and limits

`git diff --check` and local Markdown link checks passed. Review found no
actionable issue in the documentation diff: the support claim is limited to
the executed APIs and links to dated source/server evidence. The PR changes
only documentation. Human review remains pending.

These are small-data source-build integration checks. Packaged wheels, other
platforms, Flight SQL, Redis Cloud, cluster topologies and performance were
not validated. No broader compatibility or performance claim follows from
these runs.
