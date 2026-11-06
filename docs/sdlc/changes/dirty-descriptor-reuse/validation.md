# Dirty descriptor reuse validation

Date: 2026-11-06. Issue [#336](https://github.com/redis/redis-adbc-driver/issues/336).
Baseline production: `617e282ac2a06c57af9865799ec711ab670becc6` (merged PR #346).
Candidate production: `fc10e0d27847f01e6772a1514fb32d37901c030a`.
See [intent](intent.md), [contract](spec.md) and [plan](plan.md).

## Implementation and regression

`snapshotDirtyTables` formerly decoded a scoped descriptor GET for its identity
check, then called `getTable`, which fetched/decoded the same descriptor again.
It now applies the shared local hydration/policy helper to the first decoded
value and calls the complete `validateSnapshotTable` proof. The ordinary
`getTableMetadata` path uses the same extracted policy statements in the same
order. No new Lua, permission, format, cache or write/input replay is introduced.

Index sealing and final validation still call the function independently and
read fresh server bytes. Only the redundant fetch within each call is removed.
The read-time marker precedes that actual read, conservatively preserving rekey
fences. Deferred-index visibility, catalog OID hydration, compatibility refusals,
logical identity, access/segment/provenance proofs and shared-source validation
remain intact. The native GET still performs its existing authorization checks.

A baseline-only controlled fault refuses a second descriptor GET. Unchanged
`617e282a` fails the new reuse/hydration/freshness regression in **0.08 s**, while
its six refusal subcases and parent pass (seven pass events). This reproduces a
redundant fetch; it is not a claim that ordinary baseline credentials fail.
The corrected candidate passes both complete new tests under the race detector
(**0.08 s** hydration/freshness; **0.48 s** refusal parent). These require:

- Catalog/default/check OID hydration and authoritative-directory visibility for
  a deferred private index even when the persisted descriptor lacks those flags.
- A changed private descriptor between publication stages must be read afresh;
  read-time markers must be nonzero and bounded by the call. Code inspection
  confirms the marker is captured before the authorized read.
- First-read errors, malformed JSON, identity mismatch, invalid access identity
  and incompatible plain-index metadata still refuse; removed metadata skips.

## Controlled attribution

The existing opt-in fixed profile covers a 20,000-row UPDATE target and one-/512-row
Arrow appends, with **0/32 unrelated empty tables**. Workers are disabled and each
target stays below the eight-segment admission limit. All target rows, exact
BIGINT values above 2^53 and unrelated tables are independently verified. This
is foreground attribution, not sustained-compaction throughput.

| Case | Baseline round trips/op | Candidate round trips/op | Baseline GETs/op | Candidate GETs/op |
|---|---:|---:|---:|---:|
| Point UPDATE | 60.5 | 58.5 | 7 | 5 |
| Zero-affected UPDATE | 46.25 | 46.25 | 5 | 5 |
| One-row Arrow append | 60.5 | 58.5 | 8 | 6 |
| 512-row Arrow append | 63 | 61 | 8 | 6 |

Both catalog sizes show the same two-GET reduction in publishing writes; the
zero-affected control is unchanged because it bypasses publication. Four-sample
averages include other per-operation maintenance, hence fractional values.
The [summary](attribution-summary.json) and lossless [raw phases](attribution.json.gz)
retain every instrumented sample/category. Use `gzip -dc attribution.json.gz` to
read the raw JSON. Do not treat these instrumented timings as native latency gains.

## Correctness and native validation

All checks use owned **Redis Software 8.2.6-25 / database Redis 8.8.0**, verified
active/effective **4x QPF**, strict data TLS and noeviction. The fixture has three
configured primary shards on one Software node behind its proxy.

- Focused metadata/publication/conflict/Arrow/alias and new descriptor selection:
  **43 pass events / no skips**.
- Complete root snapshot race selection: **559 pass events / 44 explicit skips**,
  no failures or race reports. Compiled `go test -race -c` binary runs with
  `-test.count=1 -test.parallel=1 -test.timeout=30m -test.v -test.run Snapshot`.
- Actual Software-principal SQL RBAC races: **21 pass events / no skips**.
- Native C/ADBC snapshot validation: **19 tests / no skips, 10.576 s**.
- Full official native validation: **565 tests + 3 subtests passed / 8 capability
  skips, 75.53 s**, official validation revision
  `47a5ddf3f2a617ac88da3891848e000c74d55b0e`, Python 3.14.10.
- All six executable unit packages pass with live endpoints unset; root
  **27.420 s**. Integration skips are not deployment evidence.

The actual Cluster API/special-permission/profile skips remain explicit; this is
not new promotion/AOF/RDB coverage. The separate recovery issue #345 remains open.
The full native suite also checks ordinary loader callers affected by extracting
the shared local policy; there is no new Flex or compatibility feature here.

Both libraries are built with `make -C go build`, embed **Go 1.27.3** and clean
VCS identities, and are preserved privately:

| Library | SHA-256 |
|---|---|
| Baseline `617e282a` | `ef3892b197a10122dbdbae825b23a9cc9ac2aebb38dc56a794dbf4e5d1708d72` |
| Candidate `fc10e0d2` | `1d1042e18fa66d1a85d49acb036201985a5a5dc052f4bada96150aae856025ef` |

The first Make invocation reused a previous library embedding `cbb5b0f4` (its
production source matches the squash baseline). Embedded-revision inspection
caught this before native comparison. The baseline was rebuilt from a clean
isolated `617e282a` checkout; **no native comparison uses the stale library**.
The baseline controlled profile is a separately compiled original-source test
binary, not that shared library. The temporary baseline test copy reproduced the
red case without changing production files.

## Matched public native comparison

[All samples, source/artifact/harness hashes, settings and census results](native-comparison.json).
Twelve independent processes run baseline → candidate → candidate → baseline
for small writes with **0 and 32 unrelated tables**, plus 512-row SQL/Arrow
batches. Each case has one warmup and seven measured samples/process: **14
samples/source/case**, **280 measured operations** overall. Small cases begin
with one row; the controlled profile above separately uses a 20,000-row target.

Timing includes publication and complete RETURNING fetch where present.
Independent full-row verification is outside the timer; setup is excluded.
Every result is checked, with **zero caller retries**. Normal maintenance and
snapshot protections remain enabled. All local builds/tests/profiles finish
before timing; 500 ms and per-process census checks report **zero known competing
build/test/profile processes across 62 observations**. Each process verifies
Software QPF4/noeviction and strict TLS before timing; no extra Redis probes run
inside timing windows. Normal server indexing/maintenance can still add noise;
physical runtime shard placement is not sampled during timing.

| Workload | Unrelated tables | Baseline median ms | Candidate median ms | Change |
|---|---:|---:|---:|---:|
| Metadata COMMENT | 0 | 13.831 | 13.121 | -5.13% |
| Point UPDATE | 0 | 24.492 | 23.505 | -4.03% |
| Zero-affected UPDATE | 0 | 14.043 | 14.212 | +1.20% |
| One-row Arrow append | 0 | 20.048 | 19.987 | -0.30% |
| Metadata COMMENT | 32 | 13.464 | 13.208 | -1.90% |
| Point UPDATE | 32 | 24.209 | 23.819 | -1.61% |
| Zero-affected UPDATE | 32 | 14.588 | 13.984 | -4.14% |
| One-row Arrow append | 32 | 21.542 | 20.309 | -5.72% |
| 512-row SQL insert | 0 | 26.931 | 26.316 | -2.28% |
| 512-row Arrow append | 0 | 27.079 | 26.532 | -2.02% |

Negative change is lower latency. Every sample, including the +1.20% no-op
regression, is retained. The no-op control has unchanged I/O, yet varies in both
directions; this illustrates noise in these limited samples. The firm result is
two fewer descriptor GETs/round trips in publishing writes. Point medians are
1.6–4.0% lower here; Arrow/batch differences are modest and do not prove a stable
general speedup, sustained throughput, larger-catalog scaling or performance parity.

Host: Apple M5 Max, 18 logical CPUs / 36 GiB; Docker 18 CPUs /
25,420,967,936 bytes. Exact image digest and toolchain details are in the raw
comparison. Root-shell Go is 1.27.2; embedded library builds use Go 1.27.3.

## Review and publication status

Agent review covers ordinary-loader behavior, dirty-stage freshness, identity,
hydration/compatibility, leases/permissions, no replay and source/evidence mapping.
No remaining actionable finding is identified in this delta. This is not a formal
human approval. No test assertion is relaxed or skip broadened.

Owned cleanup is complete and verified: one disposable container and its network
are removed, with no volumes. The temporary isolated baseline checkout is
removed; both versioned libraries, regression logs and operational records remain
private. No unrelated resources or supplied license files are removed.

These are local results at the stated source revisions. Publication/review and
hosted checks must be recorded separately for the published PR head; no unfinished
or absent hosted job is counted as a successful platform check.
