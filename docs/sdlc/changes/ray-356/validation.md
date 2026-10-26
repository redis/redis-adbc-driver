# Redis ADBC → Arrow → Ray Data validation

Date: 2026-10-20. Author/reviewer: Codex. Human review pending.
Source commit: f5e90d8 plus `python/tests/test_ray_arrow.py`.
See [intent](intent.md), [spec](spec.md), and [plan](plan.md).

## Result

**Validated:** SQL queried through Redis ADBC, fetched as a PyArrow table and
passed to `ray.data.from_arrow()` works in the tested local configuration.
Three tests passed in 10.62 seconds with no skips or failures. Ray emitted
three deprecation warnings about its default DataContext settings.

| Component | Tested version/configuration |
|---|---|
| Host | macOS arm64 |
| Python | 3.12.14 |
| Ray | 2.59.0, actual local runtime with two CPUs; local mode disabled |
| PyArrow | 26.0.0 |
| ADBC driver manager | 1.12.0 |
| Redis | Existing standalone `redis:8.6.2` container with Search, port 16379 |
| Redis ADBC | Native library built from the current source checkout |

Verified cases:

- `SELECT 1 AS one`: Ray Dataset has exactly `[{"one": 1}]`.
- Real Redis table: int64, Unicode/empty strings, decimal128(12,2), booleans,
  date32, timestamps with microsecond precision and binary bytes, with NULLs.
  Ray's fetched Arrow blocks exactly match the ADBC table's schema and values.
- The same typed query with `WHERE false`: zero rows and the complete Arrow
  schema are preserved.
- An identity `map_batches(..., batch_format="pyarrow")` runs in a normal Ray
  worker and preserves the nonempty result.

ADBC handles close before `from_arrow()` is called. Fixtures use unique names
and are removed in `finally`; the test runtime calls `ray.shutdown()`.

## Reproduction

Install `ray[data]==2.59.0`, `adbc-driver-manager==1.12.0`,
`pyarrow==26.0.0` and pytest into a Python 3.12 environment, then:

```sh
make -C go build
PYTHONPATH=python \
REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.dylib" \
REDIS_URI=redis://localhost:16379/0 \
RAY_USAGE_STATS_ENABLED=0 \
python -m pytest -v python/tests/test_ray_arrow.py
```

Use the `.so` library suffix for Linux. That configuration was not tested here.
The tests skip at import when optional Ray is absent; the successful run above
installed Ray and executed all three cases.

## Scope and review

This proves a working small-data bridge. The complete database result is
materialized in the caller and copied into Ray's object store. It is not
worker-side Redis ingestion and does not use ADBC partition descriptors.
No multi-node Ray, Redis Cloud/Software, installed release wheel or performance
benchmark was exercised. The dedicated datasource and upstream Ray proposal
remain pending under #356.

The review checked closed-handle lifetime, complete result consumption, Arrow
schema/value fidelity, empty results, fixture cleanup and normal worker
execution. No remaining actionable finding was identified for this bridge.
This local self-review is not a human approval or upstream endorsement.
