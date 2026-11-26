# Plan

2026-11-26 (base main `ac82cfa0` with `0a955736`); see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Write `test_flex_compaction.py` against the real RESP3 `FT.INFO` shape. Confirm the harness tests fail without the gate.
2. Add `flex_compaction.py`, the setup gate and per-query recheck in `run.py`, and the record field in `isolation.py`. Update the README.
3. Provision a disposable Flex fixture with `go/flex-setup.sh`: own Compose project and ports, no image pull, 64 GiB with an 8 GiB RAM tier, 20 GiB container cap. Apply Search disk settings 8% / 2% / 3 MiB and AOF every second, and verify active 4x QPF through the API.
4. Build the Linux ARM64 library from main.
5. Load SSB SF1 with `run.py ssb --load-only`. Let the gate wait for every table's first GC cycle, and check it against the shard log.
6. The strict gate refused: `date_` and `supplier` got no first cycle. At the requesting session's direction, add an opt-in, recorded exemption for indexes of at most 10,000 documents, with tests first. Opt in for this fixture, with shard-log `FT.CREATE` times, and re-verify QPF.
7. Run `supervise.py ssb --reuse-loaded` for all 13 queries.
8. Record the results and compaction state against the 2026-11-26 run, re-verify QPF, and remove the fixture.
