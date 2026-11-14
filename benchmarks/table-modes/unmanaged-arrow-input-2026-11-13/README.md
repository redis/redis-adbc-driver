# Scoped unmanaged Arrow input

This fresh candidate passes its [predeclared gate](../../../docs/sdlc/changes/unmanaged-arrow-input/spec.md),
so the implementation remains applied for review. All four unmanaged elapsed
medians decreased (0.27–7.47%); geometric-mean speedup was **4.22%**. The weakest
unmanaged four-reader/4096 case was essentially flat at −0.27%. The managed
four-reader/512 control had a **4.67% higher median**, with overlapping ranges;
that does not trigger the original >3% plus nonoverlap control gate. That same
control's first-batch median was 7.50% higher, also with overlapping ranges.
These facts are part of the decision, not exceptions introduced after timing. All native RSS
ranges overlap and median differences are −0.63%..+0.28%. This is not a universal
or isolated causal speedup claim.

The prior broad experiment remains [unapplied](../direct-arrow-2026-11-13/README.md).
This is a new source, separately declared experiment and complete matched run;
no prior observations were relabeled or selected to satisfy its gate.

## Implementation and correctness

Only explicit unmanaged output, the existing ≥64-column width and disabled
optional byte budget can allocate separate fused input bindings. Compatible
integer and variable-width string outputs read pointers into the existing scratch
row and append to owned Arrow builders. Other types retain typed coercion/append.
Managed typedArrowColumn layout/bindArrowColumn and the managed per-column loop
remain unchanged; its only extra hot-path check is one nil-slice branch per row.
Narrow, byte-budget and missing iterator/metadata cases bind no fused functions.
There is no row staging, borrowed result buffer, new public option or retrieval,
predicate, visibility/lease/ACL/cancellation change.

Measured source `67264020` passed unit and race selections each with 24 top-level /
70 including subtests, zero skips/failures. Software passed all ten requested
top-level / 27 including subtests, zero skips/failures. They cover exact >2^53
integers, NULLs/defaults, aliases/overflow/NaN/temporal fallback, map inputs and
managed positional controls, OFFSET 3 LIMIT 67 with 65-row records, required
metadata errors after LIMIT, retained ownership, late-column output errors and
finish/release/cancel. New string fixtures use Unicode and literal backslash-x00
text; they do not themselves establish zero-byte coverage.

A separately labeled test/docs-only follow-up `1e112158` proves actual NUL values
using string([]byte{'a', 0, 'b'}) and an explicit zero-byte assertion. Unit, race
and Software each passed one new test with zero skips/failures. The 64-column unit
and 105-column live test assert eligible fused bindings, exact retained zero-byte
strings after subsequent reads/release, the complete 257-ID live set and unchanged
application source. All **270 production Go/module files** are byte-identical
to the measured source ([proof](production-identity.json)). Native binaries were
not rebuilt or relabeled; this adds coverage, not a repeated performance attempt.
The older report's demonstrated NUL wording is corrected without changing its
frozen source, measurements or attempt records. [checks.json](checks.json) retains
every selected case and raw private log hash. These are focused checks, not a
claim that the complete repository suite ran.

## Complete fresh native comparison

All **112 trials** independently validated every physical type/value, 100K row
count and multiplicity: baseline/candidate × managed/unmanaged × one/four readers
× 512/4096 output batch ceilings × warmup plus six measured repetitions. Every
warmup and sample remains in [native-results.json](native-results.json). There
were zero failed trials, retries, discarded outliers or post-timing source/guard
changes. Four readers execute eight disjoint ordinal tasks.

|Mode|Readers|Batch|Baseline median s|Candidate median s|Elapsed change|Baseline median RSS MiB|Candidate median RSS MiB|
|---|---:|---:|---:|---:|---:|---:|---:|
|managed|1|512|2.3397|2.2698|-2.99%|251.63|251.53|
|managed|1|4096|2.3259|2.2962|-1.27%|254.64|254.45|
|managed|4|512|1.5228|1.5939|+4.67%|252.21|252.15|
|managed|4|4096|1.5153|1.4740|-2.73%|256.18|254.56|
|unmanaged|1|512|5.2808|5.0293|-4.76%|255.83|255.79|
|unmanaged|1|4096|5.0300|4.8512|-3.55%|261.52|262.26|
|unmanaged|4|512|1.9462|1.8009|-7.47%|261.83|261.40|
|unmanaged|4|4096|1.7339|1.7291|-0.27%|265.49|264.91|

No baseline/candidate elapsed ranges are disjoint in the slower direction;
all RSS ranges overlap. The +4.67% managed control remains visible despite the
formal gate passing. All six measured values/ranges and first-batch distributions
are in the same result file. RSS is the largest single worker peak including
Python/Arrow/the driver, not summed worker memory or Go retained heap.

Both binaries use the exact same included [two-mode wrapper](read_two_modes.py)
and unchanged full-value read workers. Full source loading, fixture/index setup,
readiness and preflight verification are outside trial clocks. Spawned worker
startup/imports, schema/source loading, SQL fetch, IPC writing and worker teardown
are inside; canonical output validation is afterward. The full baseline block
ran before candidate, with reversed case order between repetitions. Shared-host
ordering/cache/scheduling and small samples limit attribution. The native source
is NULL-free; separate correctness cases supply NULL/default/error and actual
zero-byte coverage. No cross-mode ownership/consistency equivalence is implied.

## Construction diagnostic

The control uses the original typed setup and no fused bindings; the candidate
also pays its separate input binding setup, all inside each timed operation.
Both use the real unmanaged projection decoder, fresh scratch/builders and full
output construction/release, with full-field generic reference checks before/after
timing. Raw input maps are prepared outside timing. This differs from the older
broad study's managed synthetic decoder and shared candidate-binding setup, so
absolute cross-study micro timings are not directly comparable.

|Columns|Rows|Control ns/op|Fused ns/op|Change|Control B/op|Fused B/op|Control allocs/op|Fused allocs/op|
|---:|---:|---:|---:|---:|---:|---:|---:|---:|
|8|63|57,257|56,613|-1.1%|19,760|19,760|142|142|
|8|1024|857,917|873,456|+1.8%|200,240|200,240|158|158|
|105|63|615,463|434,509|-29.4%|280,417|290,608|2,111|2,217|
|105|1024|9,320,120|6,398,825|-31.3%|2,644,712|2,654,899|2,319|2,425|

Wide construction median time decreased 29.4–31.3%, but each operation adds 106
allocations and about 10.2 KB for bindings. Narrow arms use the identical fallback
and differ by −1.1% / +1.8%, with identical allocations. All 24 samples and raw
non-operational benchmark output are retained in [construction-results.json](construction-results.json)
and [construction.txt](construction.txt). Micro results alone did not authorize
adoption; the fresh full native gate did. GOMAXPROCS=4, 200 ms/sample, three repeats.

## Environment, reproduction and cleanup

Baseline `06c76c0b68258862d091487c3eb280b961f4fb8d`, version
`v0.1.0-dev.scoped-base`; measured candidate `67264020f524f62a431ef00585a218c6a63660f5`,
version `v0.1.0-dev.unmanaged-arrow-1`, clean Makefile Go 1.27.3 builds with
vcs.modified=false. The supplemental test/docs source is separately labeled above.
[provenance.json](provenance.json) retains source, library/CLI, input, controller,
wrapper and raw-log hashes. Native GOMAXPROCS/GOFLAGS/GOWORK were removed;
GOGC/GOMEMLIMIT/GODEBUG were unset. Tests/builds use GOMAXPROCS=4.

Actual [environment](environment.json): Software API 8.2.6-25 / Redis 8.8.0 /
Search 80808, active/effective 4x QPF, six workers, mnp/32 connections, one 4-GiB
noeviction RAM shard, strict data TLS certificate/hostname verification and RESP3.
Owned container limits are four CPUs/eight GiB; fixed local image identity is
recorded privately, without tag resolution or a registry digest claim. Host:
Apple M5 Max, 18 logical CPUs, 36 GiB, macOS 26.7.3 arm64. Management bootstrap uses
its separate self-signed trust mode.

Reproduce from the exact source commits with unique versioned Makefile builds,
the immutable input SHA and fresh private work directories. Provision through
make -C go redis-up and independently verify effective settings before any tests.
Privately configure REDIS_BENCH_* and SSL_CERT_FILE, set PERF_READ_HARNESS to the
unchanged benchmarks/table-modes directory, then run the included wrapper with
--suite reads --read-repeats 6, exact --source/--driver/--cli/--version and a fresh
--work. Its ownership-checked reset is outside timed trials and it provisions
nothing. Reproduce construction with BenchmarkUnmanagedArrowInputConstruction,
-benchmem -count=3 -benchtime=200ms; run explicit-zero-byte follow-ups from their
separately recorded source. Retain every new attempt and its actual identities.

Both sibling and parent source reviews found no blockers before execution; agent
review is not human approval. Every loan ended empty/index-free, all clients and
processes closed, and effective QPF/TLS/RESP3 reverified ([loan proof](loan-return.json)).
Parent owns final shared infrastructure/TLS/credential teardown, which is pending
other tracks. Operational identifiers, credentials and raw logs remain private.
No ClickBench or alternative server/provider was used.

Read-only audit: `python3 benchmarks/table-modes/unmanaged_arrow_input_audit.py`.
