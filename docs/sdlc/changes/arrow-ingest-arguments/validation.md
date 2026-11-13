# Validation

November 13, 2026. Status: evaluated and rejected; production defaults unchanged.

Source review identified repeated vector growth and per-cell
field boxing/formatting in existing writers. The candidate leaves Arrow Value
conversion, whole-slice validation, input ownership and write semantics intact.
The parent approved isolating argument construction before a broader conversion
design; this is not elimination of the Value matrix.

## Initial correctness

Focused unit/race command, both server endpoints unset:
`go test -count=1 -race -p 1 -json -run '^Test(Ingest|SnapshotBatchScriptWire)' -timeout=3m .`
passed 80 tests/subtests, zero skips/failures/races. Exact-image cases check
physical/renamed fields, NULL/empty/binary strings, exact integers above 2^53,
NaN/date encodings, import provenance and added missing fields, text boolean
vocabulary, retained argument-slot ownership and equivalent mixed-type byte
accounting. Existing coercion and Arrow string-payload ownership cases passed.

The parent explicitly loaned its owned disposable Software database. Reverified
active/effective 4x QPF, six Search workers, mnp/32 connections, one 4-GiB
noeviction RAM shard, certificate-verified data TLS and RESP3 immediately before
live checks. The fixture originated through `make -C go redis-up`; the local
management API retains its bootstrap self-signed certificate.

Live `go test -count=1 -race -p 1 -json` passed 128 tests/subtests, zero failures
or races, with one intentional `TestUnmanagedPerformanceSample` opt-in skip.
The selection covered all `TestUnmanaged` cases plus managed Arrow atomicity,
large records, batch outcomes, script-cache behavior, ingest admission and its
lost-reply/cancellation modes, defaults, CHECK, lengths and session time zone.
No restricted-principal or separate Cluster-topology claim is made by this run.
After Go exited, removed indexes/data only in the loaned database and asserted
DBSIZE zero and no Search indexes. All clients closed; Compose/TLS ownership
remained with the parent. The raw exact command and logs remain private.

## Isolated argument construction

After every correctness process/client exited, ran
`GOMAXPROCS=4 go test -count=3 -p 1 -run '^$' -bench '^BenchmarkIngestArgumentAssembly$' -benchmem -benchtime=200ms .`.
All 72 observations across 24 cases completed. Each case validates every output
against the actual baseline assembly before and after timing; all inputs and
warm/calibration work are common. Arguments retain independent owned slots.

Median per 256 rows × 105 non-NULL public columns:

| Writer assembly | Baseline ms | Candidate ms | Baseline bytes | Candidate bytes | Baseline allocations | Candidate allocations |
|---|---:|---:|---:|---:|---:|---:|
| Managed | 0.847 | 0.687 | 3,108,241 | 1,609,104 | 47,210 | 45,418 |
| Unmanaged | 2.581 | 1.026 | 4,067,673 | 1,609,236 | 128,257 | 45,419 |

All twelve width/density/writer comparisons are retained, including a negative:
managed 105-column/95%-NULL median was 0.106ms baseline versus 0.114ms candidate
(7.5% greater time), despite 45.2% fewer allocated bytes. No tuning followed
that observation. The other eleven candidate medians were lower. These short
repetitions do not establish statistical significance or native throughput/RSS.

Timing includes value encoding, field/argument allocation, per-slice field-name
boxing and the unmanaged established byte estimator. Preconverted Value rows,
column metadata, page/result scaffolding and row-ID strings are outside timing.
Arrow conversion, network/TLS, Redis/server work, hooks, allocation/publication
and Python/native process costs are excluded. Byte counts are Go allocations,
not retained memory or worker RSS. [Every sample and source/hardware/hash](assembly-results.json)
is retained. There were no failed/discarded timing attempts.



## Complete native evaluation and rejection

Baseline `5f759300c12f5191a36d2f2b5a29555ef14ef486` was built as `v0.1.0-dev.merged-perf-2`;
candidate `483f8cebf7cdb85199c40cb99e2ec1b3735a67d6` as `v0.1.0-dev.ingest-args`.
The source archive also retains the later test-only single-row control; its
production files match the measured candidate. Exact library/CLI/input/harness
hashes and every source identity are in [native-results.json](native-results.json).

Ran unchanged `arrow_compare.py --suite writes`: 40 baseline and 40 candidate
trials. All 80 completed, fully validated, with zero conflicts, failed trials,
automatic/caller retries or discarded observations. Each cell has a labeled
warmup and three measured repetitions. The typed input contains 100K×105 rows/
columns; each timed write uses its first 10K rows and up to 1K rows/call. One
writer makes ten calls, four writers twelve total; managed admitted calls
serialize. SQL and Arrow were both checked because they share these writers.

| Path | Writers | Baseline s | Candidate s | Candidate elapsed change |
|---|---:|---:|---:|---:|
| Managed SQL | 1 | 8.377 | 8.340 | -0.4% |
| Managed SQL | 4 | 19.086 | 18.810 | -1.4% |
| Unmanaged SQL | 1 | 2.618 | 2.582 | -1.4% |
| Unmanaged SQL | 4 | 1.308 | 1.375 | +5.1% |
| Managed Arrow | 1 | 8.395 | 8.258 | -1.6% |
| Managed Arrow | 4 | 17.795 | 18.004 | +1.2% |
| Unmanaged Arrow | 1 | 2.063 | 2.041 | -1.1% |
| Unmanaged Arrow | 4 | 1.212 | 1.333 | +10.0% |
| redis-py+hiredis HSET | 1 | 1.218 | 1.229 | +0.9% |
| redis-py+hiredis HSET | 4 | 0.884 | 0.895 | +1.3% |

There is no established native throughput gain. Four-writer unmanaged Arrow
took 10.0% greater median elapsed time, and SQL 5.1% greater; the unchanged
four-writer raw control was 1.3% greater. The mechanism remains unexplained;
do not attribute the four-writer result solely to field caching, sizing or
the extra NULL-count pass. Native RSS medians/min-max timing ranges and all
individual outcomes are retained, including lower unmanaged Arrow worker RSS.

The entire baseline block preceded the candidate block; this is not randomized
crossover or confidence-interval evidence. Cases alternate order by repetition.
Native builds/setup completed before timing; other heavy work from this task was
paused. The shared workstation can have unrelated activity. Historical full-matrix
results remain unchanged and are not relabeled with this shorter write-only run.

Existing-table table/index setup is outside timing. Worker startup/imports,
source loading/conversion, key/SQL construction, writes, shutdown and readiness
are inside. Complete independent SQL/raw type/value/count/multiplicity checks
are outside. Raw HSET and driver writes have different duplicate-key/publication
semantics; none of the elapsed gap is assigned entirely to conversion.

Used the parent-owned Software fixture with the same 4-CPU/8-GiB container and
one 4-GiB noeviction RAM shard. Verified active/effective 4x QPF, six Search
workers, mnp/32 connections and strict data TLS/RESP3 before and after each block;
the unchanged harness rechecks QPF before each case. Go1.27.3 darwin/arm64,
Apple M5 Max/18 logical CPUs/36GiB, same Python/PyArrow/ADBC/redis/hiredis
versions as recorded by each result. The management API retains its bootstrap
self-signed certificate.

## Added single-row scope control

After native measurement exited, source inspection identified unconditional
whole-schema unmanaged field boxing as a risk for very small sparse writes.
Added a separate test-only control for one valid row, 8/105 columns and dense/
one-non-NULL-field shapes; every unmanaged input has a non-NULL explicit key
and at least one populated public field. Actual once-per-slice boxing is
inside every timed operation. All 48 samples across 16 cases validated exact
arguments before and after timing, with no failed/discarded attempts.

| One-row case | Baseline ns | Candidate ns | Baseline B | Candidate B | Baseline allocations | Candidate allocations |
|---|---:|---:|---:|---:|---:|---:|
| 105 dense managed | 5245.0 | 4306.0 | 15,600 | 9,744 | 290 | 283 |
| 105 dense unmanaged | 10488.0 | 5519.0 | 15,844 | 9,762 | 501 | 284 |
| 105 one_nonnull managed | 1779.0 | 1799.0 | 3,616 | 3,584 | 111 | 110 |
| 105 one_nonnull unmanaged | 578.4 | 2096.0 | 405 | 3,633 | 11 | 111 |
| 8 dense managed | 500.3 | 367.7 | 1,472 | 768 | 28 | 24 |
| 8 dense unmanaged | 950.9 | 484.6 | 1,568 | 832 | 45 | 25 |
| 8 one_nonnull managed | 184.6 | 178.8 | 384 | 352 | 13 | 12 |
| 8 one_nonnull unmanaged | 237.5 | 248.7 | 384 | 416 | 10 | 13 |

The 105-column one-field unmanaged case increased from 578.4ns/405B/11
allocations to 2096ns/3633B/111 allocations: about 1.52μs and 3228B more per
argument-build operation. This is an actual isolated regression, not a claim
about whole network-write latency. Dense improvements do not erase it.
All samples and timing limits are in [single-row-results.json](single-row-results.json).

## Reproduction, checks and ownership

Apply [candidate.patch](candidate.patch) only in a separate clean checkout at
`3213aa54`, then use the repository Software workflow and exact versions/hashes
above. Native inputs/raw operational logs are private. The unapplied patch includes
all production/test changes, including both isolated benchmark fixtures.
`git apply --check --whitespace=error-all candidate.patch` passed against that
baseline. The file retains intentional nested diff context; repository whitespace
checking excludes only that patch, with applicability checked separately.

`python3 docs/sdlc/changes/arrow-ingest-arguments/audit.py` recomputes every native
and isolated distribution, exact matrix/repetition coverage, complete status,
source/version/input/QPF and patch SHA. No production rebuild or server test is
required for this evidence-only branch.

Each native block returned the explicitly loaned database empty (DBSIZE0 and no
Search indexes) after processes/clients exited. The single-row control used no
Redis. Compose/network/TLS ownership stayed with the parent coordinator, which
will perform final fixture cleanup. No other server, container, network, source
dataset or credential file was removed by this agent. Credentials/endpoints/
infrastructure identifiers/raw logs remain private. No100M ClickBench or alternate
provider was used.
