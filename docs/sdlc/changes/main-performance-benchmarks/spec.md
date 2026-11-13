# Contract

Exact native library/CLI from f630e117201752b6ccf8c2dcb4da1d3e17203c3a, built
as unreleased v0.0.26-dev.main-perf, with GetInfo/source/binary verification.
Dedicated Redis Software, one RAM shard, active verified 4x QPF/six workers,
mnp/32 connections, 4 GiB/noeviction, TLS/RESP3. Same typed source and complete
result validation as prior measurements.

100K x 105 reads: one/four readers, 512/4096 batch ceilings, warmup plus six
measured trials per case. 10K x 105 writes: matched SQL/Arrow/HSET, one/four
writers, up to 1K rows per call, warmup plus three trials. Existing-table
preparation excluded; separate creation workloads include schema/index setup
with the documented managed versus membership-only index differences. Preserve
all attempts; never replay inputs or assign success timings to incomplete work.
Historical measurements remain version/settings-labeled. No isolated PR speedup,
100M, physical Flex or Flight SQL claim. Keep operational artifacts private.

See [intent](intent.md), [plan](plan.md).
