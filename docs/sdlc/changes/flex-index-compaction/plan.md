# Plan

2026-11-26 (base main `844d8123`, then `ac82cfa0`); see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Provision a disposable Flex database with `go/flex-setup.sh`: own Compose project and ports, the local image without a pull, the SF1 shape (64 GiB, 8 GiB RAM tier), verified 4x QPF, and the 8% / 2% / 3 MiB Search disk settings of the earlier runs.
2. Load 600,000-row samples of SSB `lineorder` and TPC-H `lineitem` through the driver's Arrow `create` path, as `benchmarks/olap-release/run.py` does. Compare `FT.INFO` and per-term cost.
3. Rebuild an index over the same rows with an `FT.CREATE` initial scan. Read `FT.PROFILE`, the per-index disk store's options and log, and the shard log.
4. Follow the change in layout to the Search disk GC. Read its trigger in the public RediSearch 8.8 / 8.8-rse sources, then time the first GC for every population path on 20,000-row indexes (driver-free), under both 60% and 8% memory limits.
5. Load the full SF1 `lineorder` (6,001,171 rows) and time terms before and after its first GC.
6. Record the cause, what was ruled out, and what stays open. No driver path produces the layout, so there is no driver fix and no SSB rerun (see [validation](validation.md#driver-change)).
7. Remove the fixture and record its cost and ownership.
