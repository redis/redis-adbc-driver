# Interrupted live-mode ClickBench preparation — 2026-11-18

The user cancelled this attempt during live registration and requested AWS
teardown. **No ClickBench queries ran, and there are no query latency or
correctness results.** The planned scope was all 43 canonical queries, with one
isolated attempt per query. This does not resolve the unmanaged aggregation
failures in [#493](https://github.com/redis/redis-adbc-driver/issues/493),
[#494](https://github.com/redis/redis-adbc-driver/issues/494),
[#495](https://github.com/redis/redis-adbc-driver/issues/495), or
[#496](https://github.com/redis/redis-adbc-driver/issues/496).

## Environment and intended measurement

- Pinned driver source `7c092094`, version `v0.0.29+live.7c092094`, built using
  Go 1.27.4 and the repository Makefile. Library and CLI digests appear in the
  [sanitized summary](summary.json).
- The user selected the existing issues 493–496 AWS validation instance:
  `c8g.metal-48xl`, 192 logical CPUs / 384 GiB RAM. Redis Software 8.2.6-58,
  Redis 8.8.0, three primary shards, 280 GiB database budget, noeviction.
- Management API readback verified active 4× QPF, six Search workers,
  `mnp` routing and 32 connections before registration.
- Same-host Ubuntu Linux aarch64 client through a loopback Docker-published
  endpoint, CA/hostname-verified TLS and RESP3.
- All 99,997,497 existing application HASHes and 105 canonical columns from
  the previously verified full-scale backup were retained. No ingestion ran.
  Live registration used explicit native types and the same ten predicate
  index columns as the restored unmanaged registration.
- This attempt used the explicitly selected retained **RAM** reproduction.
  The pinned live implementation rejects Flex; this is not a Flex benchmark
  or a change to the default full-scale Flex rerun policy.
- Planned query contract: execute through complete Arrow fetch, a 300-second
  wall deadline and 86,918 MiB client cgroup limit, no swap, fresh process and
  scratch directory per attempt. Only independently validated results would
  receive timings. These query limits were never exercised in this attempt.

## Observed stopping point

The final registration progress message reported **83,383,438 HASHes read**
(about 83.4% of the source). This is a progress checkpoint, not a completed or
independently validated registration row count. The driver's full source
inspection had not finished, so live index preparation, publication, live schema
and first/middle/last source-value probes, and all query trials remained pending.

The registration attempt began at 02:52:25 UTC and was cancelled at 05:37:32 UTC
on November 19 (November 18 Pacific time). This interval is interrupted setup
work, not a completed registration benchmark, query latency or loading time.

All three live-run services were verified inactive after cancellation. Current
logs, configuration provenance and the cancellation checkpoint were archived
privately before instance termination was requested. The source backup was
preserved. Infrastructure identifiers, artifact locations, raw operational logs,
credentials and licenses are excluded from this report.

## Continuing another day

Restore a verified compatible full-scale backup into a dedicated environment
and verify effective QPF and topology again. Rebuild or verify the selected
live driver, then start a new registration: the interrupted inspection has no
resumable checkpoint. Complete index readiness, all 105 schema types and
first/middle/last source-value checks before running queries. Preserve the
canonical expressions, full Arrow fetch, fixed resource limits and independent
reference validation, including LIMIT tie checks where needed.

See the [intent](../../../docs/sdlc/changes/live-clickbench-attempt/intent.md),
[specification](../../../docs/sdlc/changes/live-clickbench-attempt/spec.md) and
[verification record](../../../docs/sdlc/changes/live-clickbench-attempt/plan.md).
