# Validation

[Intent](intent.md), [specification](spec.md), [plan](plan.md).
Related [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).

## Guarded-reader attribution before replacement

Source `55f3d91f` ran the opt-in retrieval-only profile through the root-owned
verified Software 4× QPF slot. This is Go source retrieval, not SQL, Search,
Arrow or the Python/C boundary. Every selected source byte/count/sum and every
full native row field was independently validated. One warmup plus three
measured alternating rounds, 32 × 10K-row reads/trial on the 100K/four-column +
ordinal fixture, one/four clients, all completed successfully. Logs are private.

The earlier digest/body request-budget change is present in this diagnostic
source, but the fixture's ordinary short keys still fit the 1000-command batch
limit. No claim that budgeting explains the standard concurrency regression.
Endpoint INFO CPU fields stayed zero, while some network-statistic observations
were cached/unchanged: these cannot attribute CPU or exact bytes and are not
zero server work. Global Go allocation deltas and hook command counts retain
their diagnostic scope. Trial wall seconds include independent validation;
operation samples stop before it and are not saturated-capacity evidence.

## Native candidate and actual full SQL verification

The native proposal supersedes the digest/body changes and removes the Lua
path rather than leaving dead script helpers. Frozen native source
`859a9510` (full identity in the archived provenance) passed the focused Software
race selection: **23 top-level PASS / zero SKIP / zero FAIL**. This includes
real large values, source changes between all-NULL HMGET and HGETALL, int64/NUL/
empty values, ownership, cancellation and strict unknown/malformed outcomes.
The digest prototype's older unit result is not relabeled as this native check.

Both non-race diagnostic binaries were built before timing. Identical frozen
profile source compared guarded baseline `164db2c2` (PR #439 production plus
test-only instrumentation) and native `859a9510` in **A1/B1/B2/A2** order. All
**32 records / 1,024 SQL queries**, including every warmup, fully fetched typed
Arrow COUNT/SUM and validated the independent arithmetic oracle. Each source/
client combination retains 192 measured operation samples. Full selected data,
comparison arithmetic, source/harness/binary hashes and post-run binary checks
are in the [report](../../../../benchmarks/unmanaged-projection/2026-11-14/report.md)
and [complete diagnostic archive](../../../../benchmarks/unmanaged-projection/2026-11-14/sql.json).

Pooled p50: one client **99.064→77.224 ms (1.28×)**; four clients
**176.492→98.245 ms (1.80×)**. P95: 115.454→92.171 and 206.532→129.343 ms.
Allocated bytes per 32-query trial fall about 5.6%. Per-trial source fetches
change from 320,000 EVALSHA_RO to 320,000 native HMGET; fanout is unchanged.
Endpoint CPU/cached network counters remain unavailable causal attribution.
These are instrumented Go ADBC SQL results with a workerless fixture and
GOMAXPROCS=4, not the Python/C-boundary native matrix or saturated capacity.

Sparse/wide workloads are not measured by this dense non-NULL fixture. An
all-selected-NULL row pays a second HGETALL and may fetch large unselected
fields, so it can be slower than the retired guarded path. This tradeoff remains
explicit; no sparse/wide speedup is inferred. All data retain live row semantics.

Independent source review found no actionable correctness/security findings.
All Go processes/clients exited, and the owned snapshot harness cleared its
data/indexes before releasing the coordinated Software slot. Raw operational
artifacts remain private. No archived evidence or old source identity has been
changed. Human maintainer review and approval are not inferred.

## Later key-selector compatibility source

After frozen `859a9510` measurements, source review identified that selectors can
allow HGETALL for a source key while HMGET is granted for another key pattern.
Exact native HMGET key-permission refusals now retain HGETALL compatibility too;
its own native command still authorizes the actual read. Spellings were verified
against Redis 8.8's `getAclErrorMessage`, not guessed from arbitrary NOPERM text.
Authored regressions cover generic/verbose exact-key denials, per-row fallback,
wrapped/joined key denials plus lost replies, a later unknown outer outcome and
unrecognized/different-key/channel refusals. This source-only follow-up was not
run during another agent's timing slot; parent final unit/race checks and native
matrix must use the integrated final source. Earlier 23-pass checks and SQL
observations retain their actual `859a9510` identity.

A subsequent parent review caught that key-specific denial must not disable
HMGET for every later key. The follow-up distinguishes command-wide denial from
key denial and adds a first-key-boundary regression: HGETALL-only key B falls
back once; following HMGET-only key A still uses HMGET and must never issue
HGETALL. Generic and verbose native key-refusal forms are covered. This source/
unit-only follow-up awaits parent checks and does not relabel frozen859 timings.

## Final integrated verification

The [combined follow-up validation](../performance-followup-20261114/validation.md)
records final guarded source 3f9cf6a8, integrated race/unit/snapshot outcomes, all 42
final native comparisons and ownership cleanup. Earlier pilot/diagnostic sources
and limits above remain unchanged; they are not relabeled as the integrated build.
