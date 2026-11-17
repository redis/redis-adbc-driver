# Native point-read proof attribution, 2026-11-17

This advances [#444](https://github.com/redis/redis-adbc-driver/issues/444)'s
**native attribution** task. It makes no shipping optimization or speedup claim.
The [method and reproduction](../../native_proofs.md) identify the private build
insertions and their timing/ownership boundaries.

Source: clean runtime baseline `55da25218f3606f2f8fb04d74f527903870805a4`,
Go 1.27.4, Arrow Go v18.9.0. The instrumented library is an explicitly modified
private archive, not a clean shipping binary. Actual source-overlay, builder,
runner and binary hashes are in [all final observations](observations.json).
Linux/aarch64 Python 3.11.2, ADBC manager 1.13.0, PyArrow 26.0.0, redis-py 8.1.0.
Redis Software 8.8.0, three shards, active/effective 4x QPF, six Search workers,
mnp32, noeviction; verified before and after. The native Linux client shared the
owned Software container's network namespace and used trusted localhost TLS and
explicit RESP3. This differs from host-published-port historical runs.

One client, 64 rows per mode, changing literal point predicates, one retained
warmup plus 32 measured samples per mode. Each native execute completely consumes
its result. Every exact typed row and the full final SQL fixture passed, including
NULL, UTF-8/NUL and integers beyond 2^53. Independent unmanaged raw HASH values and
key-set preservation passed. Scoped source/schema/index cleanup passed.

Every measured managed read had **10 execution-context hook calls / 19 commands**;
every unmanaged read had **10 / 18**. Each also had one unmarked `control_release`
call during the complete native operation. Unmarked does not mean maintenance:
this is lease cleanup, and its time must not be charged again as independent
background work. All 66 samples had zero dropped calls and zero in-flight calls
at End, including zero execution-context calls still in flight.

The ten foreground call positions were stable in both modes:

| Position | Categories (one pipeline per row) | Managed hook p50 ms | Unmanaged hook p50 ms |
|---|---|---:|---:|
| 1 | control_acquire | 0.1509 | 0.1798 |
| 2 | hget, zcard, metadata_root_hlen, control_valid | 0.1454 | 0.1706 |
| 3 | metadata_root_hgetall | 0.2308 | 0.2228 |
| 4 | hmget, metadata_bucket_hlen, metadata_directory_zcard, metadata_bucket_hget | 0.1649 | 0.1598 |
| 5 | hmget, exists, metadata_object_get | 0.1243 | 0.1390 |
| 6 | ft.info plus managed zcard | 0.3743 | 0.3794 |
| 7 | control_valid | 0.1262 | 0.1573 |
| 8 | hmget row projection | 0.1118 | 0.1420 |
| 9 | control_valid | 0.1260 | 0.1555 |
| 10 | control_valid | 0.1248 | 0.1311 |

These are per-position instrumented client-hook medians, **not packet RTTs or
server CPU**. Do not sum the medians, subtract them from native medians, or merge
them with the separate Go test samples. Context association can include inherited
work; it is not proof of synchronous/exclusive execution. Work starting before
Begin is outside this observer's start-boundary scope.

Instrumented native execute p50 was 2.5642 ms managed and 3.0127 ms unmanaged;
complete-fetch p50 was 0.0288/0.0436 ms. These narrow, observer-bearing samples
lack uninstrumented controls and are not comparable with historical latency
results, the point-read target screen or the full CRUD suite. No independent
10% gate, p95/throughput acceptance or regression claim is made.

The observed root→bucket→descriptor dependency chain remains. Fresh bounded
root validation precedes root materialization, and descriptor values identify
subsequent keys. The row/lease checks cover distinct before-read, after-read and
statement-completion points. This evidence does not authorize removing them,
introducing cross-statement hints, undeclared script keys or uncertain replay.
No candidate was selected and the rejected routing-hint experiment stays rejected.

[All attempts](attempts.json) retain build/runtime failures and hashes of earlier
normal observations. The final separate non-race `-d=checkptr=2` native library
built, then aborted in `main.createHandle` before any native sample. That is the
existing invalid handle conversion addressed by subsequently merged [PR #462](https://github.com/redis/redis-adbc-driver/pull/462).
The separate race attempt failed during ThreadSanitizer allocation before samples;
it does not establish a race pass or the cause of that allocation failure. No
checkptr/race bypass, dependency substitution or production GC change was made.
These checks do not qualify #446's full ownership/sanitizer/memory gates.

Private fixture resources were exclusively owned. They are torn down after the
attempts, including abrupt native failures; unrelated fixtures remain untouched.
Credentials, certificates, container IDs, raw errors and operational manifests
are not published.

## Post-merge checkptr follow-up

After PR #462 merged, the **same probe/runner** was rebuilt from
`292ee2544fa6d48baec7091eab9e2cc28f385a3d` with `-d=checkptr=2` and unchanged
forced GC. [All 66 native point-read observations](postmerge-checkptr.json)
passed: complete typed rows, both full fixture oracles, raw source preservation,
scoped cleanup and before/after effective QPF checks. No observation was dropped,
unfinished or marked with an error. This clears the reproduced `createHandle`
checkptr blocker for this real-driver point-read path. It is a **separate source
and correctness check**, not an A/B latency comparison, full ownership proof,
race pass or qualification of Arrow nested/sliced lifetimes.

The historical pre-merge abort and ThreadSanitizer allocation failure above
remain retained. #446 still needs its complete gates and #444 its independent
performance qualification. Replay the standard-library artifact checks with:

```sh
python3 benchmarks/crud-execution/results/native-proofs-2026-11-17/audit.py
```

For review, both published JSON payloads compact whitespace and place samples
last. They contain every observation; original private serialization hashes and
published byte hashes are retained in `attempts.json`.
