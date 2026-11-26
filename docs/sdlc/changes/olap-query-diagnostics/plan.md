# Plan

2026-11-26, base main `7c83c486`. See [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Read the harness, the driver's diagnostics sampler (`go/execution_diagnostics.go`) and every plan it records, and check which plan fields carry literals or names.
2. On a disposable Software Flex fixture, find which `INFO` sections the database endpoint answers and whether any of them carries the RAM/flash split (spec, "Flex RAM/flash object split").
3. Write failing tests in `benchmarks/olap-release/test_diagnostics.py`: whitelist, INFO parsing, warm-up-only instrumentation in real child processes, crash sample, option refusal, retention, third-engine unavailability, Flex-only capture order, and pass order.
4. Implement:
   - `diagnostics.py`, engine-free: the whitelist, sidecar reading and INFO sampling.
   - `isolation.py`: warm-up option in the Redis worker; `pass_order`, the split before each Redis-backed engine, the sidecar read after the worker; retention of the raw sidecar.
   - `flex_compaction.py`: share the RESP3 client constructor, without changing behavior.
5. Document the fields in `benchmarks/olap-release/README.md`.
6. Run `python3 -m unittest discover -s benchmarks/olap-release -p 'test_*.py'`.
7. Live smoke run: TPC-H SF1 loaded on a disposable Flex fixture (64 GiB, 8 GiB RAM tier, verified 4x QPF, Search disk 8% / 2% / 3 MiB, AOF every second). Then the compaction gate and queries 11, 14 and 15 in one output folder; Q15 is the #717 timeout. Record the fields in [validation](validation.md).
8. Remove the fixture and record its cost and ownership. Open the PR.
