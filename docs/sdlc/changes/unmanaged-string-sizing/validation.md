# Validation

November 13, 2026. Status: rejected as default; production unchanged.

The preceding combined candidate remains rejected and all
its native/isolated/single-row negative samples are preserved separately.
This narrower source changes only unmanaged command byte accounting; managed
writes and original field/command construction remain exactly baseline.

## Correctness

With both server endpoints unset, ran
`go test -count=1 -race -p 1 -json -timeout=3m -run '^TestUnmanagedInsertCommandBytesEquivalent$|^TestIngestColumn(Coercion|OwnsStringBytes)$' .`.
All 59 tests/subtests passed, zero skips/failures/races. The new test compares
the exact established byte formula for nil/future controls, string aliases,
NUL/Unicode/invalid-UTF8/empty strings, 1000 deterministic arbitrary-binary
commands and a larger binary string. Existing Arrow coercion and owned-string
payload checks passed; no Arrow conversion behavior changed.

The parent loaned an idle/empty owned Software fixture created with
`make -C go redis-up`. Immediately before live tests, reverified active/effective
4x QPF, six Search workers, mnp/32 connections, one 4-GiB noeviction RAM shard,
certificate/hostname-verified data TLS and RESP3. The local management API
retains its fixture bootstrap self-signed certificate.

`go test -count=1 -race -p 1 -json -timeout=5m -run '^TestUnmanaged' .`
passed 101 tests/subtests, with one intentional `TestUnmanagedPerformanceSample`
timed-diagnostic opt-in skip, zero failures or races. This includes live source
ownership, external encodings, Arrow/SQL writes, duplicate/partial/unknown
outcomes, creation and source-preserving drop contracts. No separate Cluster
topology or restricted-principal claim is made by this selection.

After Go exited, dropped indexes and reset only the explicitly loaned database;
asserted DBSIZE zero and no Search indexes. All clients/subprocesses closed.
Parent retains ownership of its Compose/network/TLS resources; this agent did
not remove them or any unrelated resource. Raw logs, credentials and operational
identifiers remain private. [Check counts and raw-log checksums](checks.json)
are retained without endpoints or generated secrets.

## Isolated construction and sizing

After correctness exited and the coordinator granted a separate exclusive slot,
ran `GOMAXPROCS=4 go test -count=3 -p 1 -run '^$' -bench '^BenchmarkUnmanagedArgumentPhases$' -benchmem -benchtime=200ms .`.
All 336 observations across 112 named cases passed complete argument/byte
validation before and after timing, with zero failed or discarded attempts.
Source remained clean/frozen at `16e3d7b1` throughout.
The [complete factored samples](factor-results.json) retain file/raw hashes,
compiler settings, fixtures, boundaries and every distribution.

The 16 input groups cover 1/256 rows ×8/105 columns ×dense/half-NULL target/
95%-NULL target/one-non-NULL-field shapes. The final public field is always
non-NULL and every source key explicit, so even sparse fixtures obey the
nonempty-HASH rule. Target percentages are not exact for every narrow fixture.
Three combined variants retain the unchanged baseline, sizing-only candidate
and earlier rejected v1. Construction and sizing also run independently; only
the estimator is production code, while the construction control copies the
unchanged actual production loop. V1 field boxing is inside its timed operation.

Combined argument construction+sizing medians for representative 105-column cases:

| Rows/shape | Baseline ns | Sizing-only ns | Baseline B | Sizing-only B | Baseline allocations | Sizing-only allocations |
|---|---:|---:|---:|---:|---:|---:|
| 1/dense | 10,186.0 | 5,683.0 | 15,855 | 13,779 | 501 | 288 |
| 1/one_nonnull | 543.4 | 427.6 | 400 | 256 | 11 | 6 |
| 256/dense | 2,499,421.0 | 1,329,236.0 | 4,067,651 | 3,526,854 | 128,257 | 73,729 |
| 256/one_nonnull | 154,256.0 | 126,809.0 | 102,410 | 65,542 | 2,816 | 1,536 |

All 16 sizing-only combined median comparisons used less time, allocation bytes
and allocations than their baseline controls. This does not establish native
throughput, RSS or statistical significance. For 256×105 dense inputs, the pure
sizing control was 1,186,959ns baseline versus 32,873ns fast, with 0 timed fast
allocations; construction remains shared/baseline. These independent phase
medians are not additive/subtractive exclusive CPU or wall-time attribution.

The retained v1 one-row/105-column/one-field combined control was 1990ns/3633B/
111 allocations, versus this sizing-only 427.6ns/256B/6 allocations. This does not
erase the original rejected v1 native or isolated negative evidence in
[PR #422](https://github.com/redis/redis-adbc-driver/pull/422); all old attempts stay
under their original conditions and source labels. The four-writer native v1
regression mechanism remains unexplained.

Preconverted Value rows, row-ID strings, metadata and shared output scaffolding
are outside timing. Combined includes argument/value encoding and byte accounting;
v1 per-slice field boxing is included. Pure sizing uses identical prebuilt
command pages. Network/TLS/Redis, Arrow conversion, source preflight, hooks,
publication and Python/native process/RSS costs are excluded. Case order is fixed,
three short observations per case, not randomized crossover/confidence intervals.
Other heavy task work was paused; unrelated shared-workstation effects remain.

## Native evaluation and rejection

Baseline `5f759300c12f5191a36d2f2b5a29555ef14ef486` used `v0.1.0-dev.merged-perf-2` and
candidate `16e3d7b17ef340199a76d0d46980440d88280e4f` used `v0.1.0-dev.string-sizing`.
Both library/CLI/input/harness hashes and all 48 individual native outcomes are
retained in [native-results.json](native-results.json). Exact Go 1.27.3 VCS
revision/clean-build metadata and baseline archive hashes were verified before
timing. Build/setup preceded timing; no candidate source changed during the run.

`unmanaged_write_sizing.py` selects the established unchanged write routines for
unmanaged SQL/Arrow and raw HSET at one/four writers. All 24 baseline and 24
candidate trials fully validated, zero failures/conflicts/retries/discards.
Each case has a separate warmup plus three measured samples. Input is the same
typed 100K×105 source, each write uses its first 10K rows and up to 1K rows/call.
Managed code remained baseline, so unaffected managed workloads were not rerun.

| Path | Writers | Baseline s | Candidate s | Elapsed change | Baseline max-worker RSS MiB | Candidate MiB |
|---|---:|---:|---:|---:|---:|---:|
| Unmanaged SQL | 1 | 2.563 | 2.511 | -2.0% | 284.3 | 288.5 |
| Unmanaged SQL | 4 | 1.410 | 1.386 | -1.7% | 274.7 | 275.0 |
| Unmanaged Arrow | 1 | 2.001 | 1.993 | -0.4% | 250.0 | 250.0 |
| Unmanaged Arrow | 4 | 1.198 | 1.267 | +5.8% | 240.0 | 241.8 |
| redis-py+hiredis HSET | 1 | 1.206 | 1.212 | +0.6% | 181.2 | 181.3 |
| redis-py+hiredis HSET | 4 | 0.945 | 0.914 | -3.3% | 181.7 | 181.5 |

There is no established broad native gain. Arrow four-writer median elapsed
increased 5.8%, while its unchanged raw control decreased 3.3%; RSS was flat or
slightly higher. SQL medians improved only 1.7–2.0% and single-writer Arrow 0.4%.
Reject the default; the mechanism behind the Arrow-four result is unexplained.
Do not assign it to the earlier cache or NULL-count pass, which this source
does not contain. No retuning or selective reruns followed.

Entire baseline block preceded candidate. Cases alternate order per repetition;
these are not randomized crossover, confidence intervals or exclusive server-CPU
attribution. The coordinator paused other heavy task work, but the workstation
is shared. All result distributions/min-max values remain retained.

Table/index setup and complete independent SQL/raw type/value/count/multiplicity
validation are outside timing. Worker startup/imports, input conversion, keys/SQL
construction, writes/shutdown and readiness are inside. Peak RSS is the maximum
individual worker high-water mark, not Go heap or summed concurrent memory.
Raw HSET and driver writes differ in duplicate-key/source ownership semantics;
the elapsed gap is not all conversion.

Verified owned Software 8.2.6 / Redis 8.8.0 / Search 80808, active/effective 4x QPF,
six workers, mnp/32 connections, one 4-GiB noeviction RAM shard, 4-CPU/8-GiB container,
strict data TLS/RESP3 before/after each block and QPF before each case. Local
management retained its bootstrap self-signed certificate. Exact package/hardware
versions and checksums are in the full observations.

## Reproduction, audit and cleanup

Apply [candidate.patch](candidate.patch) only in a separate clean checkout at
`3213aa54`. It contains the entire candidate production/test/selected-harness
diff, and passes `git apply --check --whitespace=error-all` against that baseline.
Commit the separate reproduction source before a versioned build and verify
source-file hashes; its new revision/build hash can differ from the original
measured 16e3 build. Do not apply the patch to production with this evidence.

The nested patch intentionally contains diff context; repository whitespace
checking excludes only its context, with applicability checked separately. Run
`python3 docs/sdlc/changes/unmanaged-string-sizing/audit.py` to recompute every
native/factored distribution, exact matrix/repetition coverage, source/version/
input/QPF/complete status and unapplied patch hash. Public artifact privacy and
local links were checked; raw endpoints/credentials/logs remain private.

Both native blocks reset only the explicitly loaned DB and verified DBSIZE0/
no indexes after clients/processes exited. The final 44-text-file scan across
both experiment directories found zero generated secret/URI copies. No own
test.env/TLS files were generated. Parent completed final effective-QPF/TLS/RESP3/
empty verification and removed the owned container/network and generated env/
TLS/settings/fixture-env credentials. Its [sanitized cleanup proof](cleanup.json)
confirms the pre-existing container and 31 networks survived. No 100M ClickBench,
alternate provider or unrelated resource was used/removed.
