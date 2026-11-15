# Rejected descriptor-read routing-hint prototype

**Not selected; runtime and tests remain an unapplied patch.** The predeclared gate required at least **10% less one-client point-read p50 in managed and unmanaged modes independently**, without unexplained material tail/mixed-workload regressions. The native screen missed the primary target in both modes. No opt-out flag or production routing cache is shipped.

## Complete native screen

A1/B1/B2/A2 order; 96 validated records, including 24 warmups and 72 measured trials. Each build/mode/client cell pools 192 measured operation observations. All six cases, raw redis-py controls, forward/reverse pairs and complete latency arrays are retained in the [comparison](screen/comparison.json), [CSV](screen/comparison.csv), [screen report](screen/report.md), and full [A1](screen/A1.json), [B1](screen/B1.json), [B2](screen/B2.json), [A2](screen/A2.json) records. There were no failed trial validations or discarded records.

| Mode | Clients | P50 baseline → candidate, ms | P50 less elapsed | P95 baseline → candidate, ms |
|---|---:|---:|---:|---:|
| Managed | 1 | 2.02269 → 1.83181 | 9.44% — gate missed | 2.76767 → 2.64425 |
| Unmanaged | 1 | 2.01765 → 1.87423 | 7.11% — gate missed | 3.61225 → 3.32117 |
| Managed | 4 | 4.99575 → 4.55817 | 8.76% | 8.76338 → 8.57812 |
| Unmanaged | 4 | 5.12654 → 4.86494 | 5.10% | 8.43258 → 9.26929 |
| redis-py | 1 | 0.05240 → 0.05417 | -3.38% | 0.13350 → 0.15425 |
| redis-py | 4 | 0.35800 → 0.32006 | 10.60% | 4.29983 → 4.37250 |

Unmanaged four-client p95 increased **9.92%**. Raw controls drifted in opposite directions and are not normalized away. These finite observations are not confidence intervals, saturated capacity or SLA estimates. This is a point-read-only screen, **not the full CRUD/query matrix** and not evidence of a managed mutation, aggregate or ingestion improvement. The candidate also includes the independently evaluated unmanaged projection change; this is not an isolated cache-only ablation.

Both runs used native Linux/arm64 against Redis Software with active/effective 4× QPF, six Search workers, mnp/32, one 4-GiB noeviction RAM shard and trusted TLS/RESP3. The 1K-row/four-column fixture, known-key addressing, complete typed results and unchanged source-state checks match the declared screen. Persistent client/cursor objects are outside per-operation timing; lazy transport work remains included. Exact packages/resources, library/CLI/wrapper hashes and original metric scopes remain in the frozen screen evidence.

## What the experiment changed

A per-store hint held only the previously proven bucket/value owner IDs for a short logical table suffix: at most 64 entries and 64 KiB of accounted retained suffix/ID storage. No descriptor, authority, permission or lease result was cached. An ordinary read-acquisition pin could attempt one declared-key read-only script instead of three dependent metadata resolution exchanges. Every hit freshly checked the current pinned root, bucket entry/count/directory/reference proofs, object existence/reference proofs and native GET authorization. All five physical metadata keys were explicit KEYS in the same metadata slot.

The script bounded root cardinality and MEMORY USAGE before HGETALL, graph proof HSTRLEN before HMGET, and descriptor STRLEN before GET. Limits were 256 root slots, 1 MiB root/descriptor materialization targets and 64-byte graph proof scalars; these are not a total peak-memory guarantee. Newly introduced guard refusals and stale address mismatches could fall back only before GET. Unknown/composed failures remained terminal, without replay. All permitted native commands still executed after fresh ACL preflight. Bucket corruption preceded later object errors; GET errors were deferred behind object integrity checks. Public backend status/SQLSTATE matched the original cold resolver.

Writer/private, explicit retained, shared, restricted, partition and materialized pin paths were excluded. Managed publication/admission, per-batch lease checks, SQL semantics and storage protocol 17 were unchanged. Custom principals lacking a guard capability could incur an extra read-only attempt before the native fallback; no denial was cached.

## Sources, checks and rejected attempts

- Patch application base: `1e29f5d7a5bda3e95a6e3b7dff9aa2d80e0bf578` (pre-cache source, already containing unmanaged projection). The [complete unapplied patch](candidate.patch.gz), described by [source.json](source.json), contains exactly six Go paths: snapshot.go, snapshot_metadata_cow.go, snapshot_metadata_read_hint.go, snapshot_metadata_read_hint_test.go, snapshot_metadata_read_hint_integration_test.go and store.go.
- Screen baseline: `3b42245402a2d7cf6f363faf7ac713cd36621f90`, library SHA256 `bc50f9039e55fe772d828db8042702d6d6ae24eb19ce159f22d5166d14fe98a2`.
- Final measured experimental source: `4871e159889eba104c5eb668d0718a3d67a904b6`, library SHA256 `f760230cbda582c078cf221068c6449df2d0eb70f6157b4c93c02eb9af25722c`. Version labels and both native CLI/wrapper hashes remain in [frozen-builds.json](frozen-builds.json) and the screen metadata. Later comparator/documentation commits do not relabel this frozen build.
- An initial native test at `8780f2a9dfbea35b039df9db9bdb009a92600a0c` failed because the fixture cast a driverbase-wrapped connection directly. `7f0f8234fdb7aea858ea1592bc51812185a0d401` corrected fixture unwrapping/transport observation; the revised live race suite passed 16 top-level events with zero skips/failures. The [failed fixture checkpoint](checks/fixture-initial.json) and [successful focused race checkpoint](checks/focused-race.json) remain separate. Event counts include parent/subtests; top-level denominators are provided explicitly.
- Actual Software ACL checks at `8938e8441de013ff5639f62698f3bf4018b9043b` exposed backend Lua ERR ACL formatting and failed. Final source `4871e159` replaced message parsing with fresh `redis.acl_check_cmd` plus closed permission tags. All nine native operator subcases passed (ten events including the parent), zero skips/failures: GET, EXISTS, MEMORY, HSTRLEN, STRLEN and EVALSHA denials, simultaneous bucket/object corruption, and actual cold/warm public error classification. The [initial failed ACL check](checks/native-acl-initial.json) and [final passing ACL check](checks/native-acl-final.json) are retained with every test name and explicit parent/subcase counts.
- Endpoint-free focused unit/race checks passed; native tests cover stale hints after mutation/ALTER/DROP/recreate, held old pins, original native GET behavior, exact/beyond materialization bounds, strict int64 spellings/overflow, wrong types and lost replies without replay. Full details and checkpoint identities remain in the checks records; these checks do not turn a failed performance target into adoption.

Reapply the archived patch only to its recorded base in an isolated checkout. Use a new versioned native build and an owned Software 4× QPF fixture for any future retest; never reuse these build labels for modified code. Regenerate the screen with [screen_compare.py](../screen_compare.py), supplying all four archived result files and the recorded baseline/candidate commits. The comparator must retain the complete ABBA grid and report the failed 10% target; `--require-target` intentionally returns nonzero after retaining the valid outputs. Shipping production keeps the pre-cache protocol plus the separately validated unmanaged projection change.
