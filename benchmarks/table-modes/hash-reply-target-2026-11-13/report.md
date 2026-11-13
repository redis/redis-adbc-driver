# Evaluated and rejected adaptive reply targeting as the default

The first 16-MiB adaptive managed HASH reply target reduced median peak worker
RSS by 8.9–10.8%, but single-reader elapsed time regressed by 32.7–34.0% in this
matched local comparison. **Do not adopt this candidate as the default.** This
evidence-only change leaves production fetch behavior unchanged. The candidate
implementation remains on its separate experimental branch; no smaller-target
retuning or selective sample removal followed this result.

| Readers | Batch rows | Elapsed seconds, baseline → candidate | Elapsed change | Peak worker RSS MiB, baseline → candidate | RSS change |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 512 | 2.259 → 2.996 | +32.7% | 251.7 → 225.3 | -10.5% |
| 1 | 4096 | 2.286 → 3.062 | +34.0% | 254.1 → 230.4 | -9.3% |
| 4 | 512 | 1.407 → 1.453 | +3.3% | 253.4 → 226.0 | -10.8% |
| 4 | 4096 | 1.411 → 1.394 | -1.2% | 256.9 → 234.2 | -8.9% |

The four-reader elapsed changes are small relative to the scope/noise of six
samples and do not establish an improvement. All medians and min/max values,
including first-batch latency, and **all 56 individual trials** are retained in
[results.json](results.json): each variant has four warmups plus six measured
repetitions for every 1/4-reader × 512/4096-batch case. No failed, discarded or
retried benchmark trials occurred. Warmups are labeled and excluded from medians.
RSS is the maximum worker high-water mark per trial, not total worker/process
memory, a retained-reply measurement or an allocation count.

## Exact comparison

- Baseline source: `ba855f7995b2685182ed1331bcf6703d73089c4a`, native version
  `v0.1.0-dev.merged-perf`. Archived driver/CLI/source hashes and clean Go 1.27.3
  build metadata were verified before reuse.
- Candidate source: `b3afc72367bd03dfc2f5da8a081079dde7e5e5ca`, native version
  `v0.1.0-dev.reply-target`. Production changes are at `2520eb1b`; the final
  commit only clarifies documentation. Initial checked candidate `3835c070`
  preceded conservative pagination/UNION exclusions. Build and source hashes
  are recorded in the results; neither library was rebuilt during its block.
- Same independently typed 100,000-row ×105-column Arrow IPC source and unchanged
  [managed_read_memory.py](../managed_read_memory.py) harness. Complete output
  types, values and multiplicities are validated through independent IPC output
  for every trial. Four readers execute eight independent range tasks; one reader
  executes one task. Process startup and IPC writing are included in elapsed time.
- Owned Redis Software fixture, created/reverified with `make -C go redis-up`:
  active/effective 4x QPF, six Search workers, mnp/32 connections, one 4-GiB
  noeviction RAM database, verified TLS/RESP3, 4-CPU/8-GiB container.
- Exclusive host measurement slot: baseline block first, candidate block second,
  fresh processes/work directories and owned database setup/cleanup per block.
  Harness alternates/reverses case order across repetitions. Block order was not
  randomized; host/thermal/order effects remain a limitation. These observations
  are not an isolated causal attribution or broad workload guarantee.

## Evaluated policy and correctness

The candidate keeps directory/Search discovery pages and snapshot proofs intact,
then divides their pending logical/physical keys into adaptive HMGET reply
chunks. Its initial estimate uses field width; completed chunks observe owned
strings/slots/keys, shrink immediately after large rows, and recover by at most
2x rows per completed chunk, within the existing 4096-row ceiling. It requests
no separate initial sampling round trip. The target covers estimated replies,
not key-discovery pages, Arrow buffers, allocator slack or caller-held records.
Unknown/oversized rows can overshoot; this is **not a hard memory or wire bound**.

Fewer rows per HMGET pipeline and extra reply-chunk transitions are plausible
contributors to the latency tradeoff, but were not isolated/profiled here. Do
not attribute the observed regression to round trips, decoding or accounting
without a separate controlled experiment.

The final candidate conservatively preserves original reply pages for every SQL
LIMIT/OFFSET (including consumer pagination after cap rewrites), DISTINCT outer
pagination and generic UNION leaves. The flag is set before initial fill.
Narrow fetches and scans without managed directory/candidate discovery retain
original shapes. Eligible wide managed consumers may use Arrow or generic
conversion; unlimited ordinary/partition paths remain eligible.

A baseline-equivalent private overlay disabling the new factory selection failed
the intended production-factory response-chunk regression. Initial focused race
checks passed 43 events with two expected unset-server integration skips, followed
by 35 live events with no skips/failures/races. After pagination exclusions,
focused race checks passed 49 events (two expected unset-server skips) and live
checks passed all 47 events, including LIMIT/OFFSET and existing UNION semantics.
Parent/nested test events are included in these counts. Tests cover exact row IDs
above 2^53, logical/physical alignment, absent/shadowed/tombstoned rows, empty
pages, binary/NULL/large values, final tails, malformed replies, cancellation,
retained Arrow ownership, pin/readiness/cursor lifetime and source mutation/drop.
No unsupported server or 100M ClickBench rerun was used.

After both blocks, all clients closed and the explicitly loaned fixture was
verified idle/empty (DBSIZE zero and no Search indexes). Its owner retains the
server, CA and configuration; no unrelated resources were removed. Credentials,
endpoints, raw logs, private native libraries and infrastructure IDs remain
private.

## Opt-in reproduction

Use separate clean baseline/candidate checkouts at the exact commits above, Go
1.27.3 and explicit native versions; record `go version -m` plus driver/CLI/source
SHA256. The candidate intentionally is not part of this evidence-only branch.
Use the repository Software fixture workflow and verify its effective QPF,
limits, TLS/RESP3 and empty dedicated database before each block. Configure the
existing harness's `REDIS_BENCH_*` and `SSL_CERT_FILE` variables privately.

For each version, run the unchanged harness in a fresh private work directory:

```sh
python benchmarks/table-modes/managed_read_memory.py \
  --source "$SOURCE_ARROW" --driver "$DRIVER_LIBRARY" --cli "$DRIVER_CLI" \
  --version "$EXPLICIT_VERSION" --work "$FRESH_PRIVATE_WORK" \
  --dedicated-empty-server --read-repeats 6
```

Use baseline first and candidate second to reproduce this block order; retain
all failures and warmups. Do not substitute a server/provider or a different
schema. Do not convert this experimental target into a public memory guarantee.
