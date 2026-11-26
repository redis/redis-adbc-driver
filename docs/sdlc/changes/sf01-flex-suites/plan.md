# Plan

2026-11-26 (base main `a15b1e5c`); see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Build the Linux ARM64 library from main `a15b1e5c` with `go/build-linux.sh arm64 v0.0.32-main.a15b1e5c` and record its SHA-256.
2. Provision a disposable Flex fixture with `go/flex-setup.sh`: its own Compose project and ports, the local image without a pull, a 20 GiB container, 64 GiB with an 8 GiB RAM tier. Set the Search disk settings to 8% / 2% / 3 MiB and AOF every second through the management API, and verify active 4x QPF (6 Search workers, `mnp`, 32 connections).
3. Generate the SF 0.1 sources (see [spec](spec.md#what-is-measured)) into three private profiles, one per suite, each with its own default schema (`tpch`, `tpcds`, `ssb`) in the one database.
4. Load the three suites with `run.py <suite> --load-only`. Record each index's `FT.CREATE` time from the shard log for the gate's opt-in small-index exemption (at most 10,000 documents).
5. Run `supervise.py <suite> --reuse-loaded` for TPC-H, SSB and TPC-DS, one at a time, with a management-API check before and after each suite.
6. Retain the query records and metadata in `benchmarks/olap-release/results/sf01-flex-a15b1e5c/`, record the results, and remove the fixture.
