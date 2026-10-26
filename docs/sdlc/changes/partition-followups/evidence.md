# Verification evidence

2026-10-21 (America/Los_Angeles); baseline `edbd299`; dirty working tree with
this change. Author: Codex for alberttwong. No human approval/merge/release.
See [intent](intent.md), [spec](spec.md), [plan](plan.md), the
[design](../../../partition-execution-design.md) and
[machine-readable validation summary](validation-summary.json).

## Implemented and checked

| Issue | Delivered | Limits / remaining work |
|---|---|---|
| #358 | Single typed binding row/stream, bounded exact encoding, arithmetic/casts/CASE/scalar allowlist; native independent-process exact schema/row checks | Only documented scalar types/functions; multiple parameter sets and volatile/session functions rejected |
| #359 | Per-operator capability, complete-query semantics and bounded retained-output design | No runtime global/multi-relation acceptance expanded; executor-wide budgets/spill/retention are future work |
| #360 | Explicit split strategies for application keys, foreign indexes, legacy and >=2^53 IDs; migration and ownership gates | Those runtime layouts remain rejected; no migration or application-data mutation introduced |
| #361 | Lazy disjoint cap-width ranges, fresh discovery, hidden-CONFIG caller contract, bounded requests and cursor ownership | Concurrent lowering below a live cursor's guaranteed bound is unsupported; Cloud/Software deployment proof remains open |
| #362 | Mechanism comparison, opt-in immutable result design with TTL/quota/authorization/failure/cleanup semantics; live retry regression | Retention/as-of snapshots not implemented; no exactly-once claim |
| #363 | Existing CI/release validation reuse, required native-test gate and skip artifacts, manual owned deployment matrix, benchmark harness and smoke artifacts | Linux/Software/Cloud CI has not been dispatched here; older macOS versions and production-scale performance remain unverified |
| #372 | Standard incremental string option, first discovery/all descriptors then terminal empty sets/schema, terminal errors/reset semantics, unsupported progress options | Synchronous metadata discovery; no artificially delayed work or continuous subscription |

## Environment

macOS 26.7.1 arm64; Go 1.27.2 (module declares 1.26.6); native cgo build.
Redis Open Source 8.6.2 / Search 8.6.0 in local Docker. Default standalone test
server at localhost:6379 and existing 3-node OSS Cluster API deployment at
localhost:7001. Isolated temporary standalone server at 127.0.0.1:16390 with
aggregate cap 137; separate local TLS server at 127.0.0.1:16391. No Cloud or
Redis Software was provisioned or tested. Python 3.13.16; Arrow 26.0.0;
ADBC driver-manager 1.12.0 and focused minimum-client 1.8.0 check; redis-py 8.1.0.
Configured upstream validation resolved to commit
`2400b6b20f1a200a6a206f1694b7ede0a9f041ae`.

## Commands and outcomes

Run from `go/` unless noted. `uv` was installed under
`/tmp/redis-partition-tools/bin/uv`; use any installed uv for reproduction.

- `env -u REDIS_URI go test -count=1 -p 1 ./...`: all three packages pass
  (integration skips are expected without REDIS_URI; this is not Redis evidence).
- `REDIS_URI=redis://localhost:6379/0 go test -count=1 -p 1 ./...`:
  all three packages pass, latest run main package 85.781s.
- `REDIS_URI=redis://localhost:6379/0 go test -race -count=1 -p 1 -run '^TestPartition' .`:
  pass (2.616s); dedicated CONFIG-changing test skips unless explicitly enabled.
- `REDIS_URI=redis://localhost:7001/0 go test -count=1 -p 1 -run '^TestPartition' .`:
  pass (1.172s), including independent reader ACLs and Cluster routing.
- `REDIS_URI=redis://127.0.0.1:16390/0 go test -count=1 -p 1 -run '^TestPartition(CapRanges|Reads|IncrementalLifecycle|TimeZone)$' .`:
  pass against real cap 137; sparse/direct-key/residual/empty coverage.
- Same endpoint with `REDIS_PARTITION_CAP_TEST=1 go test -count=1 -p 1 -run '^TestPartitionRuntimeCapDiscovery$' .`:
  pass while changing caps 137 -> 11 -> 3 -> 137 between reads on the same
  connection. Original configuration restored. No claim about atomic lowering
  while an already-open cursor is running.
- `make build`; from `go/validation`, `REDIS_URI=redis://localhost:6379/0 uv run pytest -q -ra --junitxml=/tmp/redis-partition-full-validation.xml tests/`:
  **369 passed, 8 skipped, 3 subtests passed**, no failures (latest run 7.14s).
  Native partition module: **7 passed**, no skips. Feature skips are recorded
  separately in validation-summary.json; they include transactions, statistics,
  secondary catalogs and unsupported connection/ingest operations.
- Against cap 137: native module excluding `connection_cancel`: **6 passed,
  1 deselected** in the initial focused run; the subsequent minimum-client
  cap-137 run included cancellation and all seven tests passed.
- `uv run --with adbc-driver-manager==1.8.0 pytest -q -ra tests/test_partitions.py`
  against the cap-137 server: **7 passed**, no skips (final run 2.04s).
- TLS native module with separate read-only `partition-worker` identity and
  explicit cap contract 137: **7 passed**, no skips. Default coordinator sets
  up/cleans data; subprocesses use the worker identity without CONFIG/DDL/write
  privileges. The local self-signed test used `skip_verify=true`; **certificate
  verification was not established**. An attempted SSL_CERT_FILE trust setup
  was rejected by macOS native trust handling. Publicly trusted/managed-service
  TLS validation remains a deployment gate.
- Final focused race checks for bindings, incremental state, live reads and
  runtime caps passed (2.256s) on the isolated cap-137 server.
- Final malformed-type canonicalization: focused Go unit regressions pass
  (0.534s); final native module **7 passed**, no skips (1.47s).
- Workflow YAML parses; required JUnit gate accepts the full native run and
  rejects missing/skipped required tests. `git diff --check` passes. Python
  sources parse and changed Markdown relative links are checked.

## Packaged library

The validation fixture accepts `REDIS_ADBC_LIBRARY` so installed package contents
can be checked by the full suite. Release CI now uses that override for Linux
wheel validation in addition to existing tarball jobs. A local macOS arm64
wheel was built and installed into `/tmp/redis-partition-wheel-env` for testing.

A direct default local build targets macOS 26 and was correctly refused by the
wheel builder for a macOS 12 tag. A deployment-target build used
`MACOSX_DEPLOYMENT_TARGET=12.0`, `CGO_CFLAGS` and `CGO_LDFLAGS` with
`-mmacosx-version-min=12.0`, and release-equivalent version ldflags. Go 1.27.2's
linker warned that its Go object targets macOS 13; testing on macOS 26 does not
establish old-OS compatibility. Release CI selects the module's toolchain;
older macOS execution still needs its own evidence.

The installed wheel's native library, selected through REDIS_ADBC_LIBRARY,
passed the full configured suite: **369 passed, 8 skipped, 3 subtests passed**
(7.42s), including all seven native partition cases. An initial development
build omitted the version ldflag and failed the upstream driver_version
assertion; the build metadata was corrected and no test was weakened.
The last type-metadata canonicalization tightening was subsequently checked by
focused Go and native regressions; the wheel/full-suite run preceded that
malformed-input-only change.

## Performance evidence

[Harness and measurement definitions](../../../../benchmarks/partitions/README.md).
Checked-in smoke artifacts:
[dense/64-byte](../../../../benchmarks/partitions/smoke-dense-64.json),
[sparse/1024-byte](../../../../benchmarks/partitions/smoke-sparse-1024.json),
[skewed/64-byte](../../../../benchmarks/partitions/smoke-skewed-64.json).

Each ingested 1000 rows, varied 1/4 partitions and 1/2 worker processes, and
verified complete exact result/schema equality. Dense retained 1000 rows;
sparse/skewed retained 100. Observed multi-process end-to-end times were about
0.14–0.20s and are dominated by startup at this size. These are single smoke
runs on shared local infrastructure, not repeated controlled performance tests.
Server counters may include other local traffic. Artifacts include library
SHA256, dirty-source provenance, client/server versions, per-process RSS,
server memory samples and network counters. No speedup, server peak-memory,
production-scale or Cloud cost claim is made.
