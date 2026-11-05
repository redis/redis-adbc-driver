# Work and verification plan

Date: 2026-11-05. Status: in progress. See [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Enable retained coordinator mode propagation and reader-only replay; run native retained/full suites on isolated three-shard Software Flex with active verified 4x QPF and retain RAM regression coverage.
2. Audit ALTER and CREATE INDEX paths; reuse compatible machinery with focused lifecycle and native tests.
3. Audit rename/rekey bounded copying and cleanup; exercise interrupted recovery.
4. Reproduce operator restart independently, then validate query recovery and separate replicated multi-node HA.
5. Check current Cloud capabilities before any further paid provisioning; run SQL only on a capable target within authorized region/budget.
6. Profile bounded-reader requests, validate improvements, repeat warm measurements and add cold/concurrent/resource evidence.
7. Publish tested changes, sanitized evidence and precise issue status.

## Scope steering

Albert narrowed work to Redis Software Flex and requested skipping issues whose server features are unavailable. #329 (Cloud) is excluded. #325 and #326 are skipped because their existing paths require unsupported FT.ALTER; no replacement schema-rebuild implementation is planned in this task. Retained replay and bounded rekey work continue using supported server operations. Skipped issues remain open.

## Completion evidence

Retained replay, canonical TRUNCATE cleanup, independent HASH rekey copying and recovery checks are implemented. Page-size optimization, 360 warm samples, 18 tiered/concurrent cases, 42 tier snapshots, sampled server peaks and the portable profiling wrapper are complete. Unit, race, native, low-cap, restart and planned multi-node failover results are in [validation](validation.md). #325/#326 remain intentionally skipped and #329 excluded. Changes require maintainer review/merge; no release approval is claimed.
