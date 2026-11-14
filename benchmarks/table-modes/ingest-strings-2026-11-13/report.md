# Owned string-column ingestion

The proposed narrow optimization passes its predeclared correctness and
quantitative allocation/time/RSS gates. Keep the production candidate for review:
validated native String/LargeString input targeting exact unbounded typeString,
at least two converted rows and at most 1 MiB of non-NULL logical column bytes.
Copy those bytes once into a Go-owned immutable string, then assign immutable
substrings to Values. Larger payloads, single rows, other types/aliases/length
constraints and encoded layouts retain the original Clone/coerce path.

This is owned copying, not Arrow borrowing, zero-copy or row-matrix reuse.
A retained scalar can retain the whole owned column buffer; the 1 MiB logical
payload cap limits that retention, not allocator or process RSS. Full validation
still precedes copying, and every pass stops at the actual conversion-row count.
NULL/empty values, sliced offsets and byte-for-byte NUL/Unicode are preserved.
Defaults, column conversion, full-slice constraints, managed whole-operation
publication, unmanaged partial acknowledgments, cancellation and no-replay rules
keep their existing order and behavior.

## Complete native results

All **64 native trials** independently validated complete 105-field physical
types/values, row counts/multiplicities and the exact 1..10000 ordinal set;
unmanaged output also passed raw HASH validation. There were zero failures,
conflicts, retries or discarded samples. Each binary ran eight cells with one
warmup and three measured repetitions. Every observation and full distribution
is retained in [native-results.json](native-results.json) and [summary.json](summary.json).

|Mode|Shape|Writers|Baseline s|Candidate s|Elapsed change|Baseline RSS MiB|Candidate RSS MiB|RSS change|
|---|---|---:|---:|---:|---:|---:|---:|---:|
|managed|one_batch|1|1.5911|1.5849|-0.39%|381.52|382.30|+0.20%|
|managed|one_batch|4|2.4318|2.4155|-0.67%|270.31|270.42|+0.04%|
|managed|one_stream|1|1.8336|1.8181|-0.84%|253.75|253.94|+0.07%|
|managed|one_stream|4|2.6578|2.6363|-0.81%|245.80|245.44|-0.15%|
|unmanaged|one_batch|1|1.2537|1.2670|+1.06%|379.48|381.80|+0.61%|
|unmanaged|one_batch|4|1.1172|1.0637|-4.79%|267.84|267.03|-0.30%|
|unmanaged|one_stream|1|1.2819|1.2344|-3.71%|252.84|252.66|-0.07%|
|unmanaged|one_stream|4|1.0760|1.0673|-0.80%|244.47|244.44|-0.01%|

Most elapsed medians were flat or lower (−4.8% to −0.4%); unmanaged one-writer
one_batch was **1.06% slower**. Its three candidate elapsed observations were
all above the baseline range, but the increase remains below the original 3%
threshold. RSS median changes range from −0.30% to +0.61%, below the original
1% threshold. This does not establish a universal native speedup or memory saving.

The gate fixed before execution rejects elapsed increases above 3% or RSS above
1% only when the candidate range is wholly above baseline. All eight cells pass.
Every tail and median remains visible, including small costs below those thresholds.
One ordered baseline block preceded one candidate block on a shared workstation;
source/cache/order variation prevents treating every difference as causal.

Both variants use the identical existing [helper](../ingest_buffer_compare.py),
archived verbatim as [helper.py.txt](helper.py.txt). Its metadata retains the
historical "conversion reuse" scope label; this candidate changes string copying
alone and does not apply the earlier matrix reuse experiment. Each worker loads the full
100K×105 source, selects its 10K-row share, adds an ordinal and, for unmanaged,
explicit application __key values. Input tables/batches are materialized. One
stream supplies 1K batches (ten for one writer, 1000/1000/500 per four-writer
share); one batch supplies 10000 or 2500 rows. Every worker makes one public call.
The optimization can apply to both shapes; pure-integer micro cases are the
unchanged conversion control. Timed work includes worker startup/imports,
source/batch preparation, connect/write/teardown. Table/index preparation,
readiness, full validation and ownership-checked reset are outside elapsed.
Source data is NULL-free; separate correctness tests cover NULL/default behavior.

## Complete construction allocation control

Every operation creates fresh row views/Value storage and converts all columns,
including owned strings. Arrays are prepared outside timing equally for both
arms. The control is the exact baseline `06c` ingestColumn function renamed in the
test; the candidate uses production ingestColumn. Generic valueAt/coerceIn
comparison checks every field before and after timing. This isolates conversion
and includes row-matrix allocation, but excludes Redis/default/constraint work;
it is not an actual native baseline binary comparison. Three 200 ms samples per
case, GOMAXPROCS=4; all **48 samples** are retained in
[construction-results.json](construction-results.json).

|Kind|Columns|Rows|Clone ns/op|Arena ns/op|Time change|Clone B/op|Arena B/op|Clone allocs/op|Arena allocs/op|
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
|integer|8|63|14594|14217|-2.6%|84224|84224|26|26|
|integer|8|1024|211703|212358|+0.3%|1273216|1273216|26|26|
|integer|105|63|174828|172631|-1.3%|1019232|1019232|317|317|
|integer|105|1024|1816092|1837821|+1.2%|16380384|16380384|317|317|
|mixed|8|63|20703|15816|-23.6%|86768|87008|194|20|
|mixed|8|1024|303600|232268|-23.5%|1340008|1322081|2906|20|
|mixed|105|63|238513|183547|-23.0%|1061384|1051840|2277|247|
|mixed|105|1024|3081416|2081780|-32.4%|17180776|17013952|33917|247|

Mixed-string allocation medians fell **89.2–99.3%**, above the fixed 50% target;
conversion elapsed medians fell **23.0–32.4%**. Mixed allocated bytes were lower
except the eight-column/63-row case (+0.28%, below the 3% gate). Integer-only
control bytes/allocation counts are identical; time medians vary −2.6% to +1.2%.
No cell failed the original micro elapsed/allocated-byte gate. Short/oversized
input fallback and shared-buffer retention remain explicit tradeoffs.

## Correctness, ownership and attempts

Corrected unit and race each passed six top-level tests / 66 cases, zero
failures/skips. Verified Software passed eight top-level / 14 cases, zero
failures/skips. Unit tests prove owned values survive input mutation and release,
including actual NUL/Unicode, String/LargeString sliced offsets, one retained
scalar, exact-cap/oversize fallback, NULL/empty and strict prefix bounds. Invalid
input offsets reject before output mutation. Existing differential coercion tests
retain unsupported-target error order and the single-row owned-clone fallback.

The new managed/unmanaged test checks all 8197 values after records sized
4096/3/4097/1, actual NUL/Unicode, exact integers above 2^53 and an omitted managed
default. Existing Software controls cover 100001-row managed atomic success/late
failure and unmanaged partial failure, unknown replies without replay, reader
failure, cancellation and changed schema. This is selected coverage, not the
entire repository suite. [checks.json](checks.json) retains every selected case.

The initial focused unit attempt failed to compile: the newly explicit []byte{0}
NUL fixture was declared const. Only that test declaration changed to a local
variable; a new source commit and explicit unit-02 attempt precede all timings.
The original compile failure/controller exit1 and raw digest remain in checks.
No test assertion, gate or production source was weakened. Runtime production
bytes stayed identical to cd44471b throughout; all timings use clean `85cf660b`.

Ownership is independent of store scheduling: Values already own copied Go
bytes before subsequent row checks and writes proceed; defaults still evaluate
before column conversion. Store insertion consumes/encodes
rows synchronously, including durable unknown-reply resolution; retaining a
command string header retains the owned buffer safely. No input Arrow buffer,
mutable builder, global pool or shared scratch is retained. Concurrent mutation
of an Arrow input during conversion is not newly supported.

## Environment, reproduction and cleanup

Baseline clean `06c76c0b68258862d091487c3eb280b961f4fb8d`, version
v0.1.0-dev.scoped-base; candidate clean `85cf660bd8bacc6ff1e2ed9d6f3289ff946bdfec`,
version v0.1.0-dev.ingest-strings-1. Both library/CLI pairs are versioned Makefile
builds using Go 1.27.3, vcs.modified=false. [provenance.json](provenance.json)
records source/binary/helper/input/controller hashes, original quantitative gates,
checks and runtime settings. Exact measured source/tests are also in
[candidate.patch](candidate.patch). Native GOMAXPROCS/GOFLAGS/GOWORK were removed;
GOGC/GOMEMLIMIT/GODEBUG were unset. Tests/build/micro use GOMAXPROCS=4.

Observed [environment](environment.json): Redis Software API 8.2.6-25, Redis 8.8.0,
Search 80808, active/effective 4x QPF, six workers, mnp/32 connections, one 4-GiB
noeviction shard, strict certificate/hostname data TLS and RESP3. Four-CPU/eight-GiB
container limits verified; fixed actual local image identity is retained privately
without a tag-resolution/registry-digest claim. Host Apple M5 Max, 18 logical CPUs,
36 GiB, macOS 26.7.3 arm64. Management bootstrap has its separate self-signed trust mode.

Provision a new owned Software fixture with make -C go redis-up; verify effective
settings before tests. Apply the exact patch to an isolated baseline; build each
binary with unique VERSION through Makefile and record clean embedded metadata.
Run the same existing helper with --write-repeats 3 and fresh private --work paths,
privately supplied REDIS_BENCH_* / SSL_CERT_FILE. Reproduce conversion control from
go/ with -run '^$' -bench '^BenchmarkIngestStringConversion$' -benchmem
-benchtime=200ms -count=3, and record every new attempt rather than reuse hashes.
The helper never provisions or tears down infrastructure. These local TLS results
do not establish remote-latency or every possible input-size performance.

Root and sibling agents reviewed source before execution, no blockers; this is
agent review, not human maintainer approval. Final evidence review is pending.
All clients/subprocesses ended and each loan returned empty/index-free with QPF,
TLS and RESP3 reverified. Root owns final shared infrastructure/credential cleanup; that teardown is pending. No child teardown is claimed.
Operational artifacts, identifiers, credentials and license material remain private.
No ClickBench or alternate Redis provider was used.

Audit without Redis or Go: python3 benchmarks/table-modes/ingest_string_audit.py.
