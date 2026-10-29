# Ray worker reader validation

Date: 2026-10-29. Author/self-reviewer: Codex. Baseline: `34ae626` plus this change.
Status: local validation passed; human review and hosted CI pending.
See [intent](intent.md), [spec](spec.md), [plan](plan.md) and
[tracker](https://github.com/redis/redis-adbc-driver/issues/164).

## Environment and results

macOS arm64; Python 3.12.14; Ray 2.59.0; PyArrow 26.0.0; ADBC manager 1.13.0.
Native library built from this checkout using `make -C go build` with the
Makefile's version linker flag and a writable Go build cache.

An owned disposable Redis Software fixture used Redis 8.6.2, one shard, Search,
active 4x QPF with effective six Search workers, mnp routing and 32 connections.
The repository setup and TLS fixture helpers verified these settings before
reads. TLS used a private CA with certificate/hostname verification; generated
credentials and operational artifacts remain outside the repository.

- Initial non-TLS suite: 14 passed in 8.85s, zero skips (before three additional
  cancellation/retry/TLS checks were added).
- Final complete TLS suite: **17 passed in 11.71s, zero skips**, three Ray
  DataContext deprecation warnings. Local two-CPU Ray runtime; local mode off.
- No multi-node, Linux execution, release wheel or performance claim. The new
  hosted CI step is configured, not a completed hosted result.

## Covered behavior

- Dataset read and downstream map_batches preserve int64, strings/Unicode,
  decimal128, bool, binary, NULLs and exact schema/value coverage across four
  partitions. Parameterized queries work.
- Empty Dataset schema is present before/after consumption even though Ray may
  omit zero-row blocks from to_arrow_refs.
- Serialized ReadTasks run in worker PIDs distinct from the caller; native
  batches never exceed the requested five rows and partitions produce multiple
  batches. Planning handles have already closed.
- Replaying descriptors observes a subsequent update, documenting live reads.
- Unsupported aggregate SQL fails during planning without fallback. Invalid
  options fail before connection creation.
- Deterministic lifecycle tests assert reader/cursor/connection close order after
  completion, early close, batch error, KeyboardInterrupt and schema mismatch.
  Advancing one batch does not fetch the next batch.
- Real worker early release and fresh read succeed; native connection cancellation
  fails the active reader, closes its connection and permits a subsequent read.
- A Ray task configured for one retry fails after its first batch, then retries
  successfully with complete rows. This does not establish exactly-once behavior
  for side effects or every Ray Data failure boundary.
- Workers read through verified TLS; removing their CA causes certificate
  rejection and a readable serialized task error.

## Reproduction

Use `make -C go redis-up` for a disposable Software fixture; it verifies 4x QPF.
Build the native library and install `./python[ray]` plus pytest in Python 3.12.

```sh
RUN_RAY_TESTS=1 RAY_USAGE_STATS_ENABLED=0 \
REDIS_URI=redis://localhost:6379/0 \
REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.dylib" \
python -m pytest -q python/tests/test_ray_datasource.py
```

Without a TLS fixture the private-CA rejection check skips. For full TLS
coverage, prepare an owned Software fixture with `scripts/software_partition_tls.py`,
set REDIS_URI from its private admin URI file and REDIS_TLS_CA_FILE to its CA
certificate. Do not commit these private files. Use `.so` on Linux.

## Self-review

Reviewed worker ownership, no whole-query fetch, opaque descriptor transport,
static coverage, empty schema metadata, native exception serialization, optional
dependency isolation and consistency/security documentation. No remaining
blocking finding identified. This self-review is not human approval or Ray
endorsement. Abrupt process death, multi-node behavior and other versions remain
explicit limitations. Existing SQL reader/writer tests are preserved unchanged.
