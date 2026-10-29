# Prefect integration plan and evidence

Date: 2026-10-29. Source commits: initial test `95d6583`, public PR base `34ae626` plus this change. Status: local checks
passed; CI configured but not run here. Issue/PR: none created.
See [intent](intent.md) and [spec](spec.md).

## Completed work

1. Add four integration tests in `python/tests/test_prefect.py` using Prefect's
   temporary API test harness, rather than invoking task functions directly.
2. Add a Prefect 3 optional extra and include it in test dependencies.
3. Include Prefect in the existing Python integration suite. A focused Redis
   Software step covers both architectures when the full matrix assigns the
   broader integration suite to OSS; routine Software integration runs discover
   these tests without duplication.
4. Document task-local ownership and retry-safe writes; update only the tested
   comparison path's claim.

## Local verification

- Host: macOS arm64; Python 3.15.0; Prefect 3.9.0; PyArrow 26.0.0; ADBC driver
  manager 1.13.0; driver built from this checkout using `make -C go build`
  (`GOCACHE` redirected to writable temporary storage).
- Isolated Docker Compose project `adbc-prefect-test`, image
  `redislabs/redis:8.2.0-78.18`, Redis Software database engine 8.6.2,
  one RAM shard with Search; endpoint `redis://localhost:16385/0`.
- Management API readback before tests: QPF active, scaling_factor=4,
  search-workers=6, sched_policy=mnp, conns=32, status=active.
- Command from repository root (with the optional extra and pytest installed):

```sh
PREFECT_HOME=/private/tmp/redis-adbc-prefect-home \
PREFECT_SERVER_ANALYTICS_ENABLED=false PREFECT_API_URL='' \
REDIS_URI=redis://localhost:16385/0 \
REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.dylib" \
/private/tmp/redis-adbc-prefect-venv/bin/python -m pytest -q \
  python/tests/test_prefect.py \
  --junitxml=/private/tmp/redis-adbc-prefect-results.xml
```

Result: **4 passed in 13.42s; zero skips**. No driver fix was needed.
After porting to public main `34ae626` and rebuilding the driver, the same
Prefect suite passed **4 tests in 9.84s, zero skips**; installer checks passed
**2 tests in 0.41s**. The public-base run used the same versions and isolated
Redis Software endpoint; its setup script verified QPF 4× before tests.
JUnit output: `/private/tmp/redis-adbc-prefect-public-results.xml`.

The expected injected task failure retries successfully; the expected absent
table leaves its flow failed and exposes the original driver exception.
Installer regression checks: **2 passed in 0.45s**. `git diff --check`, workflow
YAML parsing and relative documentation link target checks passed. The disposable
Redis container and its Compose network were removed after testing.

## Review

Self-review: checked connection/cursor cleanup, unique table cleanup, explicit
retry idempotence, full Arrow fetch and result assertions, and scoped claims.
No actionable findings. This is not human approval. Remote workers, Cloud,
Flight SQL and prefect-redis remain untested; Linux CI results are pending.
