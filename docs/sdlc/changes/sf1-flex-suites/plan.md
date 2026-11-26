# Plan

2026-11-26 (base main `7c83c486`); see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Build the Linux ARM64 library from main `7c83c486` and record its SHA-256.
2. For SSB, then TPC-H, then TPC-DS:
   1. Create a fresh Flex fixture with `go/flex-setup.sh` (64 GiB, 8 GiB RAM tier, 20 GiB container, local image without a pull). Set Search disk 8% / 2% / 3 MiB and AOF every second, and verify active 4x QPF.
   2. Load with `run.py <suite> --load-only`, then record each index's `FT.CREATE` time from the shard log for the small-index exemption.
   3. Run `supervise.py <suite> --reuse-loaded`, verifying the fixture before and after.
   4. Remove the fixture.
3. TPC-H Q15 passed in every #648 pass but timed out here. Before removing its fixture, rerun Q15 alone, once with this build and once with main `064eb12d`'s library.
4. Retain the records in `benchmarks/olap-release/results/sf1-flex-7c83c486/`, write the results, and open one issue per failing query that no open issue tracks yet.
