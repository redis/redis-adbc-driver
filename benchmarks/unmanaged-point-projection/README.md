# Native unmanaged known-key projection benchmark

This opt-in harness measures the ordinary C/Python ADBC path for known-key reads
against unmanaged registrations and redis-py+hiredis. It is a dedicated width
control, separate from the complete OLTP/OLAP scenario matrix. Source creation
alone is not evidence of a live run or a demonstrated speedup.

The default full grid is **128 records per build/window**: four payload layouts
(none, registered 64 KiB, unregistered 64 KiB, both) × one/four selected columns ×
one/four persistent clients × unmanaged/redis-py × one warmup plus three measured
rounds. Each trial has 32 calls over an immutable 1,000-row fixture. Case order
alternates by repetition. A/B or ABBA windows must retain every run with exact
build/version/harness labels; do not discard slower or failed samples.

The four selected public fields contain int64 IDs/groups, exact int64 values
above 2^53 and strings containing actual NUL bytes. The registered payload is a
fifth public field that is not selected. The unregistered payload is added only
after schema registration and includes NUL bytes. Every layout has the same
membership index definition (`id NUMERIC`); payloads are not indexed. The
existing planner still verifies the live index definition and performs adopted
prefix/nested-key and catalog/lease checks. This harness does not cover foreign
index FILTERs or Flex membership; their correctness tests remain separate.

Run inside the [native Linux client](../linux-client/README.md), with an
exclusively owned empty Redis Software database created by `make -C go redis-up`
and independently verified active/effective 4× QPF, six Search workers, mnp/32,
one 4-GiB noeviction RAM shard, trusted certificate/hostname TLS and RESP3. Supply
the normal private `REDIS_BENCH_*` and `SSL_CERT_FILE` environment. Mount the
source worktree/Git common directory so the driver revision can be verified.
No existing large Arrow fixture is needed.

```sh
python3 benchmarks/unmanaged-point-projection/run.py \
  --driver /private/exact-build/go/build/libadbc_driver_redis.so \
  --cli /private/exact-build/go/build/redis-adbc \
  --version v0.1.0-dev.example \
  --work /private/fresh-window --dedicated-empty-server
python3 benchmarks/unmanaged-point-projection/audit.py \
  --results /private/fresh-window/results.json \
  --out /private/fresh-window-summary.json
python3 -m unittest discover -s benchmarks/unmanaged-point-projection -p 'test_*.py'
```

Client/cursor objects persist for one trial; execute/full Arrow fetch or native
HMGET plus equivalent typed Arrow conversion are timed. Object setup/teardown,
fixture work and independent validation are outside operation timing. Lazy
first-operation transport work remains included, and warmup does not preconnect
later trials' clients. Whole-trial closed-loop throughput includes coordination
and scheduling; it is not a saturated capacity score. The harness has no caller
retry loop; the inherited `automatic_retries=0` metadata is a policy declaration,
not measured wire retry/reconnect telemetry.

Every result is independently checked for exact physical type/value/count.
Before and after each width, every source key and every binary field/value is
checked against an independent generator, including both payloads and the
ordinal. Selected-value and complete-source **value-byte oracles** are recorded
and independently recomputed by the stdlib auditor. They exclude field names,
RESP framing and TLS; they are not measured wire bytes. Use the separate Go
command profile to verify the driver's actual HMGET/HGETALL command selection.

All timed samples, including warmups and failed/incomplete trials, are retained
before validation. On failure, stop and inspect the owned fixture; do not rerun
SQL/input or overwrite the work directory. The private owner marker is retained
in `owner-private.json`. After all clients exit, the owner must verify that exact
marker before emptying the dedicated database and removing owned resources.
Never publish owner/endpoint/credential/certificate files or raw operational
logs. Keep historical measurements scoped to their actual source/environment.
