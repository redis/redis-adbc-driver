# Fused Arrow input: evidence only

Keep this candidate **unapplied**. The exact source/tests from `255e5891` are preserved in [candidate.patch](candidate.patch); production Go files in this evidence branch remain identical to `88774f10`. Unmanaged native medians improved 2.6–6.4%, but managed results were mostly 1.4–3.0% slower. This fails the predeclared requirement for consistent native benefit in both modes. No guard, mode default or implementation was retuned after timing.

## Candidate and correctness

The candidate adds fused coercion/append functions for already compatible integer and variable-width string outputs in the existing ≥64-column path. They read pointers into the same one-row scratch, avoiding intermediate Value returns/copies and nested append dispatch. Required metadata decoding for the full row, timezone fitting, output-column order, skips/limits and terminal error cleanup remain unchanged. Other conversions and byte-budget mode use existing coercion/append. This is copying into owned Arrow buffers, not zero-copy or a direct RESP decoder. New fixed per-column bindings can add setup allocations; no 64-row staging block is added.

Corrected focused unit and race selections each passed 21 top-level / 65 including subtests, zero skips/failures. Verified Software passed all ten requested top-level / 33 including subtests, zero skips/failures. Coverage includes exact integers above 2^53, NULLs, defaults, Unicode and literal backslash-x00 strings, NaN, overflow/aliases/temporal fallback, map/HMGET inputs, OFFSET 3 LIMIT 67 with 65-row records, required metadata errors after LIMIT, malformed width, retained record buffers and managed/unmanaged finish/release/cancel. Native input itself has no NULLs.

Coverage precision correction, November 13, 2026: the selected string fixtures
used a printable backslash-x00 sequence, so they did not establish actual NUL
coverage. This corrects the earlier wording without changing the frozen source,
measurements or attempts. A later, separately labeled
[explicit-zero-byte regression](../../../go/scan_unmanaged_arrow_nul_test.go)
checks the new unmanaged-only candidate; it is not coverage of this old binary.

Two initial endpoint-free controller selections accidentally included live filtered integration cases. Both Go runs exited zero, with four endpoint-missing skips; the strict controllers exited one. Those attempts and the exact skipped cases are retained in [checks.json](checks.json). The regex was corrected to exact pure test names and both runs repeated with source/assertions unchanged. A separate unsupported CLI `version` probe exited two; embedded Go build metadata and the native driver GetInfo supplied the version checks. No Go assertion or native trial failed.

## Complete construction control

Fresh scratch/builders and candidate binding setup are inside each timed operation; raw HASH maps are prepared outside. Both arms share the candidate setup, then disable/enable fused input. Thus this isolates dispatch savings and cannot establish baseline setup allocations or full driver speedups. Every output field is independently compared with the generic record reference before and after timing. All 24 samples are retained in [construction-results.json](construction-results.json) and [construction.txt](construction.txt).

|Columns|Rows|Control median ns/op|Fused median ns/op|Time change|Control B/op|Fused B/op|Allocs/op both|
|---:|---:|---:|---:|---:|---:|---:|---:|
|8|63|57,546|57,709|+0.3%|19,728|19,728|142|
|8|1024|904,935|907,821|+0.3%|200,208|200,208|158|
|105|63|453,424|292,325|-35.5%|291,408|291,409|2,216|
|105|1024|6,779,591|4,293,880|-36.7%|2,655,703|2,655,699|2,424|

The wide construction control improved 35.5–36.7%; narrow arms execute the same fallback and differ by about 0.3%, with identical allocations. Full native results below determine the decision.

## Actual native baseline/candidate

All 112 trials independently validated every physical type/value, row count and multiplicity: two binaries × managed/unmanaged × one/four readers × 512/4096 public batch ceilings × warmup plus six measured repetitions. Complete baseline ran first, then candidate; no outlier was discarded or failed measurement retried. Four readers execute eight disjoint tasks. Clock includes spawned workers, full SQL fetch and Arrow IPC writing; source setup/index readiness/preflight and canonical result validation are outside each trial. Every sample, first-batch time and distribution is retained in [native-results.json](native-results.json).

|Mode|Readers|Batch|Baseline median s|Candidate median s|Time change|Baseline median RSS MiB|Candidate median RSS MiB|
|---|---:|---:|---:|---:|---:|---:|---:|
|managed|1|512|2.2272|2.2942|+3.01%|252.42|252.57|
|managed|1|4096|2.2466|2.2850|+1.71%|253.89|253.21|
|managed|4|512|1.5813|1.5767|-0.29%|252.98|251.80|
|managed|4|4096|1.5170|1.5390|+1.45%|256.61|257.73|
|unmanaged|1|512|5.3101|5.0751|-4.43%|256.62|256.16|
|unmanaged|1|4096|5.1775|4.8475|-6.37%|262.38|263.12|
|unmanaged|4|512|1.9171|1.8674|-2.59%|264.48|263.06|
|unmanaged|4|4096|1.7863|1.6960|-5.06%|267.10|266.93|

RSS is the maximum single worker process peak, including Python/Arrow/driver; it is not aggregate worker RSS or Go retained heap. Median RSS deltas range from −0.54% to +0.44%; all baseline/candidate RSS ranges overlap. No native memory reduction is established. Timings are one ordered shared-host experiment, not isolated causal estimates. The strong isolated construction result does not translate consistently into both native modes. Keep the candidate unapplied under its original gate; do not adopt a narrower mode default from this experiment.

## Environment, reproduction and cleanup

Go 1.27.3; Apple M5 Max, 18 logical CPUs, 36 GiB, macOS 26.7.3 arm64. Redis Software API 8.2.6-25, Redis 8.8.0, Search 80808; four-CPU/eight-GiB container and one 4-GiB noeviction RAM shard. Active/effective 4x QPF, six Search workers, mnp/32 connections, strict certificate/hostname-verified data TLS and RESP3 were checked before/after the loans. The fixed actual local image identity stayed unchanged and is retained privately; no tag-resolution/digest-pinning claim follows. Native GOMAXPROCS is unset; tests/build/micro use GOMAXPROCS=4. Source, binaries, input and unchanged harness hashes are in [provenance.json](provenance.json); package versions remain in native metadata.

Apply candidate.patch with `git apply --unidiff-zero` to a clean isolated baseline checkout. Build through `make -C go build adbc-cli VERSION=<unique-version>` and verify clean embedded source/version metadata. Provision a new owned Software fixture with `make -C go redis-up` and verify effective 4x QPF/TLS/RESP3 before running. The exact executed [two-mode read wrapper](read_two_modes.py) is included verbatim and its hash is checked. Set `PERF_READ_HARNESS` to the isolated checkout’s benchmarks/table-modes directory; reuse its unchanged [arrow_compare.py](../arrow_compare.py) with a fresh private work directory, exact source/library/CLI/version and `--suite reads --read-repeats 6`. The wrapper restricts modes to managed/unmanaged, preserves the full read workers and performs ownership-checked reset outside trial timing. New runs must record their actual source/settings and every attempt; never relabel them with these measurements. Reproduce the isolated control with `go test -run '^$' -bench '^BenchmarkArrowInputConstruction$' -benchmem -count 3 -benchtime=200ms .` from go/.

Run the read-only evidence audit: `python3 benchmarks/table-modes/direct_arrow_audit.py`. No Redis access or Go build occurs in that audit. All clients/subprocesses ended and each loan returned empty/index-free with QPF/TLS/RESP3 reverified ([proof](loan-return.json)). Parent final cleanup completed: the owned container/network and generated TLS/test.env/settings/fixture-env credentials were removed, while the pre-existing container and 31 networks were preserved ([sanitized proof](cleanup-proof.json)). The final credential scan required zero redactions, so raw measurement log hashes remain unchanged. Raw operational logs, identifiers and credentials remain private. No ClickBench was run.

Parent and sibling agents reviewed candidate source before execution and found no blockers. These are source-only agent reviews, not human approval. The predeclared [contract](../../../docs/sdlc/changes/direct-arrow-input/spec.md) and [validation](../../../docs/sdlc/changes/direct-arrow-input/validation.md) retain the negative adoption decision.

Root reviewed evidence `4832ca45` and the exact-wrapper follow-up `2253744b`: the 136-sample audit, all eight native cells, retained attempts, report and empty production Go diff passed with no blockers. This is agent review, not human approval.
