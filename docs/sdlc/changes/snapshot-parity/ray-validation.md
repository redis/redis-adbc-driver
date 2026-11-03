# Validation: Ray shared-generation replay

Date: 2026-11-03. Parent source `f653864`; native library built from that source.
Status: local Ray compatibility checkpoint; [full parity](plan.md) remains open.

The earlier Ray test expected a serialized partition task to observe an UPDATE
when replayed. On storage protocol 12 it instead returned the original NULL,
reproducing `AssertionError: assert None == 'changed'`. That is the required
shared-generation behavior. The updated test compares the complete Arrow table,
including schema metadata, rather than checking only the changed scalar.

The test now verifies independent worker serialization and bounded Arrow batches,
stable original-generation replay after UPDATE, fresh-plan visibility of the
UPDATE, and replay of both generations after TRUNCATE and DROP. The retry test
also changes source data between an injected first-attempt failure and Ray's
second attempt; the complete second result must equal the original table.
No automatic replay of uncertain SQL writes was added. Adapter docstrings and
the integration guides describe generation lifetime and the need to replan
after expiry/release. Output remains unordered; this is not an exactly-once
processing or CDC contract.

The TLS negative test initially accepted the private certificate because workers
inherited `SSL_CERT_FILE`, even after the explicit database CA option was removed.
Changing Python's environment after loading the Go native library did not remove
Go's initialized environment. The negative task now runs in a separate Ray runtime
environment with inherited CA paths empty before native-library initialization.
The positive task still reads and validates every value with explicit CA trust;
the negative task must raise a certificate verification error.

## Verification

- `make -C go redis-up`: owned Redis Software fixture, active verified 4x QPF,
  three primary shards, noeviction, strict TLS. Database API policy and effective
  Search workers/routing were checked before testing. Private endpoints and
  credentials are deliberately omitted.
- `make -C go build`: passed, preserving the release-version linker flag.
- `PYTHONPATH=python RUN_RAY_TESTS=1 REQUIRE_RAY_TLS=1 python -m pytest -q
  python/tests/test_ray_datasource.py` with the current native library, private
  CA and dedicated fixture supplied through environment: **17 passed, zero
  skips, three Ray configuration deprecation warnings, 13.00 seconds**.
- macOS ARM64, Python 3.13.16, Ray 2.59.0, PyArrow 26.0.0, ADBC driver manager
  1.13.0. Redis Software image 8.2.0-78.18, Redis 8.6.2.
- `git diff --check`: passed.

The fixture first contained the incompatible format-11 benchmark catalog; it
was explicitly cleared in the owned disposable database, without migration.
Repeated full test runs then retained enough five-minute partition capabilities
to reach the existing 16-generation admission limit. That run ended with ten
passes and seven setup/teardown errors and is not counted as a passing run.
The reported complete run used a reset dedicated fixture. Bounded capability
retention and generation admission remain operational limits, not performance
parity claims.

## Author review and limits

Reviewed complete-value assertions, worker isolation, Arrow batching, planner
closure, replay lifetime, cancellation and TLS trust removal. No driver execution
logic changed in this checkpoint. The existing schema/type/empty-result, input
validation, lifecycle and certificate assertions remain active. Self-review is
not maintainer approval. Multi-node Ray, abrupt worker process death, other
platforms, release wheels, current Cluster API/failover/AOF recovery, native wide
fetch and broader feature/performance parity are not established by this run.
