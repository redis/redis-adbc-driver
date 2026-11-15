# Unmanaged source retrieval investigation — November 14, 2026

The field-argument boxing experiment is **not selected**. It reduces simulated
production-batch CPU/allocation, but the Go SQL comparison does not establish an
end-to-end latency gain and four-client median latency increased in both pairs.
Production `unmanaged_projection_read.go` and its existing unit file are restored
exactly to baseline `b904cf8d`. The new opt-in harnesses, evidence, source snapshots
and [experiment patch](field-args-experiment.patch) remain reviewable. The zero-context
patch applies to the baseline with `git apply --unidiff-zero`.

## Sources and execution scope

- Baseline `b904cf8d0d85686b4b1b7adc0aa72dbe954866a4` (merged #439).
- Final experiment `6b1806b2`; exact [reader source](final-unmanaged_projection_read.go)
  and [ownership regression](final-unmanaged_projection_read_test.go) archived.
- Earlier separate-template experiment `7831d016` is retained under
  `rejected-template-*`. It saved batch allocations but added a singleton
  allocation (2,304 bytes at 128 fields; micro median +5.6%). The final experiment
  instead boxes fields into the first command's owned args and copies that field
  tail into independent later command args. No source-read fanout reduction.
- [Batch provenance](batch-provenance.json), [reverse binary hashes](reverse-binaries.json),
  [effective environment](environment.json), [check counts](checks.json),
  [file hashes](sha256.json). Both native source readers preserve authoritative
  strings, NULL, type/existence, whole-pipeline errors and live-image semantics.

Software was started by the parent with `make -C go redis-up`; its helper verified
active 4× QPF, six Search workers, mnp/32 connections, one 4 GiB shard, noeviction,
strict TLS and RESP3 immediately before every live invocation. Exclusive execution
was serialized with sibling work. The host is Apple M5 Max/darwin arm64, Go 1.27.3,
GOMAXPROCS4. Private endpoint/infrastructure/license data and binaries are excluded.
The harness closes clients and attempts to drop disposable indexes and flush its
database; individual cleanup errors are ignored by that existing harness.

## Microbenchmark: allocation gain with no SQL claim

Identical production-batch transport-hook benchmark, 1/1000 keys × 2/16/128 fields,
200 ms × three repetitions per invocation, source order baseline/candidate/candidate/
baseline. Six observations per source/case; all 72 raw observations are retained
in `unmanaged-final-batch-*.log`. No server, Search, SQL, Arrow or C bridge.

| Keys | Fields | Baseline → candidate ns/op (median) | Baseline → candidate allocations |
|---:|---:|---:|---:|
| 1 | 2 | 243.9 → 248.1 | 14 → 14 |
| 1 | 16 | 628.95 → 617.0 | 30 → 30 |
| 1 | 128 | 3,326.5 → 3,260.5 | 142 → 142 |
| 1000 | 2 | 174,860.5 → 160,469.0 | 8,017 → 6,019 |
| 1000 | 16 | 530,734.5 → 419,254.5 | 24,017 → 8,033 |
| 1000 | 128 | 2,999,622.0 → 1,915,228.0 | 136,017 → 8,145 |

Singleton bytes are unchanged. Batch bytes also decrease; raw logs contain them.
This isolated result cannot establish application latency or server capacity.

## Full Go SQL: separate forward and reverse pairs

The unchanged SQL harness loads 100K synthetic rows; every query independently
validates a complete INT64 Arrow COUNT/SUM over 10K matches. One/four persistent
clients, 32 queries per trial, one warmup and three measured repetitions per
source/invocation. Setup and validation are outside individual query timing.
Every trial issues **320,064 HMGET**: 320,000 source reads plus 64 metadata reads;
source-command fanout is unchanged. Metadata/lease EVALSHA counts vary 192–207.

The forward pair used `go test`; those temporary executables were not retained.
Before the reverse pair both same-source test executables were built once and
hashed, then run directly with **no build between reverse windows**; hashes match
before/after. Sources, relevant harness, Go/runtime settings and timed workload
remain unchanged, but this is **not one frozen-binary ABBA execution**. Keep pairs
separate when assessing reproducibility. No Python/C-boundary benchmark was run.

| Pair / source order | Clients | p50 baseline → candidate ms | p95 baseline → candidate ms |
|---|---:|---:|---:|
| Forward B→C | 1 | 78.543 → 78.011 | 94.435 → 97.933 |
| Forward B→C | 4 | 84.883 → 97.201 | 120.323 → 132.665 |
| Reverse C→B | 1 | 77.171 → 78.486 | 92.713 → 96.874 |
| Reverse C→B | 4 | 90.902 → 94.999 | 140.193 → 131.188 |

All **32 records / 1024 queries** (including warmups) pass the typed result oracle;
768 measured queries total. Raw arrays are `sql-{baseline,final}.jsonl` and
`sql-reverse-{baseline,final}.jsonl`. Descriptive cross-pair summaries retain the
executable caveat. Median allocations across both pairs decrease from about
10.61M to 9.97M per 32 queries (1 client), and 10.62M to 9.98M (4 clients); bytes decrease
by about 10.3 MB/trial. Finite trials, client elapsed and command instrumentation
cannot attribute the four-client difference to server CPU, GC or one code change.
The repeated median loss and absent application win favor omitting the experiment.

## Sparse/all-NULL/wide: existing fallback cost is now visible

Each source has a complete 64-record source-retrieval matrix (512 reads, 384 measured):
256 HASHes plus one wrong-type and one missing key; eight reads/trial;1/4 clients;
one warmup and three measured repetitions, alternating mode order. Fields include
an exact `9007199254740993` and actual NUL text. Sparse cases have 26 selected pairs
present/230 absent. Wide adds an unselected 16 KiB string to every HASH.
Every returned image and native HMGET/HGETALL count is independently validated.
This excludes SQL/Search/Arrow/C bridge; the smaller fixture is not the 100K SQL test.

Selected production baseline observations (p50 ms):

| Source shape | Clients | Native HGETALL | Projected HMGET + required fallback |
|---|---:|---:|---:|
| Dense | 1 | 16.622 | 1.555 |
| Dense | 4 | 4.535 | 4.590 |
| Sparse90 | 1 | 0.482 | 1.129 |
| Sparse90 | 4 | 3.392 | 3.126 |
| All selected NULL | 1 | 0.479 | 1.361 |
| All selected NULL | 4 | 1.982 | 3.269 |
| Sparse90 + unselected 16 KiB | 1 | 20.747 | 22.031 |
| Sparse90 + unselected 16 KiB | 4 | 34.000 | 33.736 |

Both baseline/final raw matrices and summaries retain all cases, tails and warmups;
noisy controls and only 24 measured samples/case limit comparison. The all-NULL
path necessarily adds a later complete native image under the current API. For
all-NULL per read it performs 258 HMGET plus 257 HGETALL, versus 258 HGETALL in native
mode; WRONGTYPE is omitted without a second read. Wide unselected data remains a
material cost. No adaptive strategy or causal explanation for dense control timing
is inferred. The separate-template matrix is also retained as a rejected source.

Reproduce the sparse record audit and corruption checks:

```sh
python3 benchmarks/unmanaged-projection/sparse_audit.py benchmarks/unmanaged-projection/2026-11-14-field-args/sparse-baseline.jsonl
python3 -m unittest discover -s benchmarks/unmanaged-projection -p test_sparse_audit.py -v
```

## Next safe boundary

The [source API analysis](../../../docs/sdlc/changes/unmanaged-field-args-20261114/source-contract.md)
defines force-source projection plus exact integer/presence reducers already in
Search #12334's scope. Ordinary LOAD aliases do not establish exact source bytes;
existing grouped/full LOAD* must not become 100K individual reads. A future bounded
existence-only native TYPE path for **zero selected fields** is worth evaluating,
with live type/existence and ACL/error semantics preserved; no such optimization
is implemented or measured here. No upstream issue/comment was created.
