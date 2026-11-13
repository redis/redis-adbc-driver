# Validation status

November 13, 2026. Reviewable candidate based on `3213aa54`; production baseline
matches the fully measured `5f759300`. Correctness and all 56 native trials
completed successfully. Focused checks alone are not performance evidence;
the matched native observations and their limitations are recorded below.

## Regression and focused race checks

A private Go overlay inserted the baseline-equivalent residual-refusal guard
at the production projection factory. The new test compiled and then failed
`TestUnmanagedFilteredProjectionParity` because no filtered projection was
selected, as expected. This is a selection regression with the same candidate
test/types, not a claim of executing the entire old source tree. Raw logs and
the overlay are retained privately.

The candidate then passed this focused command with Redis endpoints unset:

```sh
go test -race -count=1 -p 1 -timeout=5m \
  -run '^Test(Unmanaged(Filtered|ScanProjection|Projection|WideProjection)|ScanProjection)' -json .
```

There were 15 test pass events, six expected endpoint-gated integration skips,
zero failures/race reports. Parent/nested events are counted. Cases compare
the generic evaluator's results, status/SQLSTATE and error ordering against
compiled integer/boolean predicates, typed parameters and NULLs. They retain
short circuit, malformed required fields on rejected rows, unsupported fallback,
exact integers above 2^53, binary keys, duplicate outputs, OFFSET/LIMIT,
processed/matched diagnostics and independently retained Arrow buffers.

## Verified Software live race checks

The parent explicitly loaned its dedicated owned Redis Software database.
Its `make -C go redis-up` fixture was reverified immediately before testing:
active/effective 4x QPF, six Search workers, mnp/32 connections, one 4-GiB
noeviction RAM shard and strict certificate/hostname-verified data TLS/RESP3.
No server was provisioned or alternate endpoint used by this child.

The live `go test -race -count=1 -p 1 -timeout=5m` selection passed 24 test
events, zero skips/failures/races. A 9001×105 fixture checks every selected
value/type/NULL and membership under an indexed integer range; retained records
survive Next, complete or early Release and cancellation. The live mutation test
installs the new projection before changing/deleting/retyping a discovered
source HASH and confirms stale/non-HASH rows remain excluded. Existing numeric
range, exact large-integer/index-change, filtered foreign-membership and
unfiltered ownership controls also passed. Source DROP/read cleanup checks retain
application HASHes, arbitrary application __rowid fields and the application index.

After the checks every client/process exited. Removed all test indexes/data only
from the authorized loaned database and asserted DBSIZE=0 plus FT._LIST empty.
The parent retains its container/network/TLS ownership; this child did not tear
them down. Generated environment/credentials and all raw operational artifacts
remain private. No 100M ClickBench or unsupported server/provider was used.

All outcomes, changed-source hashes and private raw-log digests are in
[checks.json](checks.json). No source changed between these candidate checks.
Broader final correctness is recorded below.

## Native comparison

Committed candidate `783711f185e1a6a63ae9365e447b8fb7a3ec981b`, built with the
explicit version `v0.1.0-dev.filtered-arrow`, completed all 28 native trials.
The archived `5f759300c12f5191a36d2f2b5a29555ef14ef486` baseline, version
`v0.1.0-dev.merged-perf-2`, completed the preceding 28 trials. Every trial passed
complete independent Arrow type/value/count/multiplicity validation. Zero failed,
retried, restarted or discarded native attempts. Both source revisions and clean
Go 1.27.3 build metadata were verified, together with exact input/library/CLI
checksums and effective four-CPU/eight-GiB container limits.

The unchanged `unmanaged_projection_read.py` harness uses the same typed
100,000×105 input, workers and complete IPC validator as the preceding unmanaged
study. Four case configurations each have one warmup and six measured trials
per variant. One reader executes one full scan; four readers execute eight
disjoint numeric ranges over fixed data. Those are independent live SQL range
queries, not shared partition snapshots. Retrieval remains LOAD* for full scans
and live HGETALL for filtered ranges; only decoded projection/predicate handling
changes. The strategy labels in raw results describe retrieval, not decoding.

Median complete-read seconds and maximum individual worker RSS (MiB):

| Readers | Batch ceiling | Baseline seconds | Candidate seconds | Baseline RSS | Candidate RSS |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 512 | 4.991 | 4.972 | 256.1 | 255.5 |
| 1 | 4096 | 4.799 | 4.794 | 262.9 | 262.7 |
| 4 | 512 | 2.049 | 1.808 | 472.9 | 263.3 |
| 4 | 4096 | 1.976 | 1.695 | 499.4 | 267.6 |

The targeted filtered four-reader workload used 11.8%/14.3% less median time and
44.3%/46.4% less maximum-worker RSS at 512/4096 ceilings. Unfiltered one-reader
controls were essentially unchanged (0.4%/0.1% lower time; no gain claimed).
These are six-sample local observations, not confidence intervals or a promise
for unsupported predicate shapes. Large literal IN lists, string/float/date
predicates and other excluded expressions retain their established generic path.

The whole baseline block ran first, then the whole candidate block; case order
alternated/reversed within each block. Other chat builds/tests/timings were
paused, but the machine is shared. Blocks were not interleaved/randomized, so
time/thermal/order effects remain. Timing includes process/import/startup,
source-schema loading, complete fetch/conversion, IPC output and shutdown.
Independent IPC validation is outside the elapsed interval. RSS is the maximum
individual worker high-water mark per trial, including Python, libraries,
source-schema loading and driver/IPC memory; it is not isolated Go heap or summed
process-tree memory. First-batch latencies and every range are retained too.

Host: Apple M5 Max, 18 logical CPUs/36 GiB, macOS arm64; Go 1.27.3, runtime-default
GOMAXPROCS (unset for both native variants). Python 3.15.2, PyArrow 26.0.1,
ADBC manager 1.13.0, redis-py 8.1.0/hiredis 3.5.0. Software 8.2.6, Redis 8.8.0,
Search module 80808; shared owned fixture settings match the live checks above.
QPF was checked before every case and at each block's end. Both blocks returned
the loaned database empty/no-index with all clients/processes closed. Parent
retains fixture/CA ownership; no additional server was created or removed.

[benchmark.json](benchmark.json) retains every one of 56 samples, exact build/
input/harness hashes, package versions and recomputed timing/RSS distributions.
Recompute coverage/provenance/arithmetic with
`python3 benchmarks/table-modes/filtered_projection_audit.py`.

## Final broader correctness

After other chat measurements stopped, the parent granted a separate
exclusive correctness gap. `GOMAXPROCS=4 go test -count=1 -p 1 -timeout=10m
-json ./...`, with all Redis endpoint variables unset, passed: 1402 test/subtest
pass events, 1278 endpoint/opt-in test skips, zero failures; seven package passes
and three no-test package skips. This server-free pass does not establish Redis
compatibility. No Go production or test source changed after the measured commit.

The shared owned fixture was reverified immediately before the final live
`GOMAXPROCS=4 go test -race -count=1 -p 1 -timeout=5m -run '^TestUnmanaged'
-json .`. It passed 112 test/subtest events, with zero failures/races and one
intentional `TestUnmanagedPerformanceSample` opt-in timing-diagnostic skip.
The existing broad unmanaged ownership, source mutation, ingestion, error,
concurrency, index and query cases passed alongside the new predicate cases.

All clients/processes then exited. The authorized loaned database was returned
empty (DBSIZE zero, FT._LIST empty); no parent-owned Compose/TLS or unrelated
resource was removed. Exact final outcomes and raw-log digests are retained in
[checks.json](checks.json). After all remaining tracks completed, the parent
reverified effective QPF/TLS/RESP3 and empty data, then removed the owned
container/network and generated TLS, environment, settings and credential files.
All one pre-existing container and 31 networks were preserved. The sanitized
[final cleanup proof](cleanup.json) is retained. No retained raw-log credential
redactions were needed, so existing log digests remain unchanged.

The all-56-sample audit passed exact coverage/build/input identity, complete
validation, effective QPF and elapsed/RSS recomputation. New public-artifact
privacy, local Markdown links and repository whitespace checks also passed.
Raw logs, generated environment values and operational identities remain private.

See [intent](intent.md), [contract](spec.md) and [plan](plan.md).
