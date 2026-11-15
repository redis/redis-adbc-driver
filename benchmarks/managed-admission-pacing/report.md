# Rejected target-local admission pacing experiment

2026-11-14. Resetting managed admission backoff after observed target progress increased command load and worsened important latency and throughput results. The production change was rejected; active runtime admission behavior remains byte-identical to baseline. No native-C improvement claim or native confirmation run is warranted for this rejected candidate.

The candidate remembered only the fresh validated target latest physical prefix and segment count. A change reset the next rejected-attempt wait to 5 ms; unchanged targets retained 5/10/20/40/80/100 ms backoff. Every attempt still acquired a fresh pin, validated the target and used existing server admission, then released the pin before waiting. There was no cached proof, retained waiting pin, SQL/input replay, new Redis command, storage-format change or FIFO guarantee. Selective compaction with a stable latest prefix was detected by reduced segment count.

Both frozen binaries were built before clean controls and timing. Blocks were B-warm/C-warm/B0/C0/C1/B1/B2/C2; each block rotated 1/4/8 writers as recorded in [provenance](provenance.json). Each trial started with 1,000 rows and interleaved 288 total disjoint INSERT/UPDATE/DELETE operations. All 24 trials (including warmups) passed complete typed final-row/value checks: **6,912 acknowledged writes, zero failures, conflicts, retries or missing admission observations**. Warmups remain in the [complete samples](observations.json.gz) and are excluded from the statistics below. Each measured operation/concurrency cell has 288 observations across three trials. Percentiles use nearest rank.

| Writers | Operation | p50 baseline → candidate ms | p95 baseline → candidate ms | Max baseline → candidate ms |
|---|---|---:|---:|---:|
| 1 | INSERT | 147.94 → 163.07 | 239.43 → 237.26 | 314.36 → 310.38 |
| 1 | UPDATE | 45.65 → 44.68 | 151.79 → 115.83 | 217.14 → 189.50 |
| 1 | DELETE | 39.22 → 39.16 | 72.51 → 68.65 | 209.36 → 210.68 |
| 4 | INSERT | 86.01 → 78.84 | 1785.77 → 1685.32 | 4166.82 → 4466.84 |
| 4 | UPDATE | 56.39 → 86.66 | 843.76 → 1143.33 | 7053.86 → 3284.87 |
| 4 | DELETE | 53.65 → 82.11 | 1126.22 → 1247.78 | 3911.06 → 3158.28 |
| 8 | INSERT | 62.76 → 107.87 | 3196.48 → 2913.37 | 12019.66 → 5142.20 |
| 8 | UPDATE | 73.90 → 81.91 | 1800.13 → 2301.16 | 17704.70 → 6355.11 |
| 8 | DELETE | 58.50 → 101.60 | 1450.34 → 2273.78 | 9383.17 → 5865.19 |

Some extreme maxima decreased, while 4/8-writer UPDATE/DELETE p95 and several medians worsened. Whole-workload completion and command cost prevent selecting the candidate based on smaller maxima alone.

| Writers | Throughput baseline → candidate ops/s | Change | Foreground commands/ack baseline → candidate | Amplification | Maintenance commands/ack baseline → candidate |
|---|---:|---:|---:|---:|---:|
| 1 | 12.367 → 12.102 | -2.14% | 94.82 → 97.26 | 1.03× | 146.00 → 146.50 |
| 4 | 12.508 → 11.575 | -7.46% | 141.03 → 255.27 | 1.81× | 176.56 → 213.57 |
| 8 | 12.587 → 12.437 | -1.19% | 184.61 → 479.80 | 2.60× | 203.66 → 311.14 |

Throughput is all measured successful operations divided by summed complete-trial wall time, with identical total work. Every four-writer candidate trial completed more slowly than its baseline pair. Full per-trial durations and per-operation admission/acquire distributions remain in [comparison.json](comparison.json).

| Writers | Attributed foreground client calls/ack baseline → candidate | Acquire attempts/ack baseline → candidate |
|---|---:|---:|
| 1 | 48.69 → 49.22 | 2.55 → 2.58 |
| 4 | 61.21 → 91.77 | 5.03 → 10.77 |
| 8 | 73.35 → 152.45 | 7.46 → 22.22 |

Client calls count observed single-command/pipeline hook invocations carrying operation context; these are not packet counts. Aggregate native command totals additionally include background-context pin release. Maintenance timelines/counts are retained separately; elapsed client time or overlap is not Redis CPU or causal attribution.

The clean held-target [unrelated-publication controls](controls.json) all passed cancellation, zero blocked execution/NEXTVAL consumption, complete unrelated rows and final comment validation. Idle and unrelated traffic each yielded 23 acquire attempts over about 2 seconds in both modes. The unrelated control used two INSERTs plus four COMMENT publications below the compaction trigger; both modes issued 398 foreground commands. This confirms the target-local policy did not amplify probes in this short control, without establishing a general fairness or CPU claim.

Initial six-INSERT controls were invalid and are [disclosed separately](invalid-controls.json): that variant could trigger unrelated compaction under a held full-catalog pin and exceeded its deadline; candidate was mistakenly launched before baseline exited, contaminating both. Both processes terminated, the disposable harness cleared data/indexes, and independent verification observed empty before clean controls. No result from those invalid controls enters the paired comparison.

The [same-fixture environment](environment.json) was observed at the end of the round: Redis Software 8.2.6-25, Redis 8.8.0, Search 80808, one 4 GiB RAM shard, active 4× QPF/six Search workers/mnp32/noeviction, certificate-verified TLS/RESP3; server container four CPUs/8 GiB; Apple M5 Max/macOS arm64 host. Configuration gates also ran before every live invocation, and no server/container replacement occurred. Infrastructure identities and credentials remain private. No ClickBench was run.

This is instrumented Go ADBC SQL execute/ack timing. An idle execution pool object is pre-created for hooks, but ordinary per-statement transport acquisition/registration/finish remains timed. Per-call observer allocation/locking can affect scheduling/GC. Paired blocks use fresh binary processes and each trial creates fresh data/clients; warmup warms server/cache rather than the later process heap. These limits require observer-free/native confirmation before shipping any future promising runtime candidate. The mixed/negative result here is sufficient to reject this candidate.

Reproduce the rejected source with `gzip -dc candidate.patch.gz | git apply` against the unchanged active runtime; the patch includes its pacing unit contract. [Executed source copies](executed-source/) and source/binary/harness hashes are retained in provenance. The active Go tree retains only diagnostic harness/control improvements. Build both binaries before timing, use an exclusive verified Software 4× QPF fixture, and run the opt-in profiles following the [SDLC plan](../../docs/sdlc/changes/managed-admission-progress-pacing/plan.md). The [comparison script](compare.py) retains incomplete/failed trial outcomes and audits the exact trial grid.

Next work should investigate an explicitly bounded, cancellable admission queue or handoff/notification contract that releases pins during waiting, rather than further increasing polling frequency. Such a design needs ownership/expiry, unknown-outcome and mixed-version semantics before a fairness guarantee.
