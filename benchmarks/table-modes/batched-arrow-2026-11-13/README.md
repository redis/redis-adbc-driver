# Batched Arrow construction: evidence only

Candidate `2caafbc3cef7005523d4f0eca1631c2646128d4e` was frozen before correctness and construction timing. It stages at most 64 complete rows for eligible wide primitive columns and uses concrete Arrow AppendValues calls. All required input decoding, timezone fitting, per-column output coercion, public batch/limit boundaries and existing fallbacks remain in their previous order. This is a bounded copying path, not zero-copy. No post-timing tuning has occurred.

## Correctness

Focused unit and race runs each passed 20 top-level tests / 58 including subtests, with zero skips/failures. Verified Software integration passed 6 top-level tests / 21 including subtests, zero skips/failures. Tests cover every mixed primitive field, exact integers above 2^53, NaNs, NULLs/historical defaults, string payloads containing NUL/Unicode, HASH positional and map decoding, duplicate aliases, naive timestamp fitting, 65-row public records, successful OFFSET 3 LIMIT 67, required-input metadata error order after LIMIT, and later-column coercion errors after 63 staged valid rows. A previously retained record remains valid after subsequent error/release. Unsupported types, narrow projections, small public records and optional byte budgets retain the scalar path.

The live runs used only the parent-owned Software fixture with verified active/effective 4x QPF, six workers, mnp routing / 32 connections, 4 GiB/noeviction, strict TLS certificate verification and RESP3. The shared database was returned empty/index-free; all clients/processes closed. Final infrastructure teardown was completed by the parent experiment owner.

## Isolated complete-construction control

All 24 samples (eight cases × three repetitions × 200 ms) are retained in construction-results.json. Timed work includes fresh schema/projection/reader/builder/bindings/staging setup, decoding/coercion and complete Arrow output construction/release. Every output field is compared to the generic reference before/after timing, including NULL and exact integer behavior. Raw map inputs are prepared outside timing for both variants. The scalar control disables staging on the same candidate source; it is baseline-equivalent conversion code, not an actual archived baseline binary. This is not an end-to-end driver throughput result.

|Columns|Rows|Scalar median ns/op|Batched median ns/op|Time change|Scalar B/op|Batched B/op|Scalar allocs/op|Batched allocs/op|
|---:|---:|---:|---:|---:|---:|---:|---:|---:|
|8|63|67,321|68,240|+1.4%|79,856|79,856|164|164|
|8|1024|937,186|917,776|-2.1%|130,128|130,128|224|224|
|105|63|764,590|709,399|-7.2%|1,189,749|1,237,506|2386|3067|
|105|1024|10,338,938|9,094,511|-12.0%|2,229,416|2,493,816|3257|3982|

Both 8-column controls use the identical scalar fallback and have identical allocation counts; their timing difference is control variation. Wide batching reduced construction time but increased observed allocated bytes by 4.0% / 11.9% and allocation counts. No lower peak RSS or full driver throughput claim follows from these measurements. The native comparison used actual clean 24f baseline and candidate binaries, both managed and unmanaged, 100K × 105, 1/4 readers, 512/4096 public batch ceilings, 6 measured plus warmup per case: 112 trials. The completed native comparison below does not satisfy the adoption gate.

## Decision and complete native comparison

Keep the candidate **unapplied**. Production Go files are identical to baseline `24f654b76b98f3d8960af94a3ca0230185d467a1`. The exact original candidate source/tests/docs are preserved in [candidate.patch](candidate.patch), measured source `2caafbc3cef7005523d4f0eca1631c2646128d4e`. The 64-row block and eligibility were fixed before measurements; no post-hoc tuning or selective default was applied.

All 112 native trials passed complete output validation: two variants × two modes × one/four readers × 512/4096 public batch ceilings × one warmup plus six measured repetitions. Baseline ran first, then candidate. All samples, warmups, first-batch times and RSS values are retained in [native-results.json](native-results.json); no outlier was discarded or trial repeated. The whole-worker timing includes process startup, full SQL fetch and Arrow IPC writes; canonical validation occurs afterward. Source load, index readiness and preflight validation are outside each trial. This is a single ordered experiment, so differences cannot all be attributed causally to the implementation.

|Mode|Readers|Batch rows|Baseline median s|Candidate median s|Time change|Baseline median RSS MiB|Candidate median RSS MiB|RSS change|
|---|---:|---:|---:|---:|---:|---:|---:|---:|
|managed|1|512|2.2684|2.2667|-0.1%|251.98|251.31|-0.3%|
|managed|1|4096|2.2504|2.2727|+1.0%|254.62|259.51|+1.9%|
|managed|4|512|1.5843|1.5434|-2.6%|251.59|251.34|-0.1%|
|managed|4|4096|1.5572|1.4780|-5.1%|256.20|262.05|+2.3%|
|unmanaged|1|512|5.2885|5.0036|-5.4%|255.88|256.03|+0.1%|
|unmanaged|1|4096|5.1183|4.8479|-5.3%|263.01|262.95|-0.02%|
|unmanaged|4|512|1.9317|1.8027|-6.7%|263.27|262.27|-0.4%|
|unmanaged|4|4096|1.7716|1.7281|-2.5%|264.39|269.82|+2.1%|

RSS is the maximum individual worker process peak, including Python, Arrow and the driver; it is not the sum of four workers or Go retained heap. The 4096-row RSS tradeoff matters: managed one-reader baseline range 253.42–256.23 MiB versus candidate 257.20–261.19 MiB, and unmanaged four-reader 256.30–266.27 versus 266.53–276.27 MiB. Both candidate ranges are entirely above baseline ranges. Managed four-reader median RSS also rose from 256.20 to 262.05 MiB. At 512 rows, median RSS differences stayed between −0.4% and +0.1%.

There are useful median time improvements: unmanaged −2.5% to −6.7%, managed four-reader −2.6% to −5.1%. Managed one-reader is essentially flat (−0.1% / +1.0%). Tails are retained: candidate unmanaged one-reader/4096 includes a 6.1013 s sample versus baseline maximum 5.7406 s, and managed four-reader/4096 includes 1.8031 s versus baseline maximum 1.5991 s. These results, higher allocation counts and the out-of-range RSS do not meet the predeclared requirement for consistent full-path benefit without a clear memory regression. No narrower mode/batch default was introduced.

The two modes have different data ownership and consistency semantics; this experiment compares each mode with its own baseline and makes no managed-versus-unmanaged throughput claim. Native input has no NULLs; the separate correctness tests exercise NULL/default/NaN/overflow/error and retained-record behavior. The unrelated raw Redis reference was run once by the parent experiment and is not a timed control in this candidate comparison.

## Provenance, checks and reproduction

See [provenance.json](provenance.json) for source, driver/CLI/input/harness hashes and settings; [checks.json](checks.json) for every completed correctness case and command; [construction.txt](construction.txt) and [construction-results.json](construction-results.json) for all isolated samples. No failed or skipped attempt occurred. The only measured candidate was the frozen original; no tuning or selective rerun followed results.

Go 1.27.3, macOS arm64 / Apple M5 Max, Software container limited to four CPUs and 8 GiB, database 4 GiB, active/effective 4x QPF, six Search workers, mnp routing / 32 connections, noeviction, strict TLS verification and RESP3. Native GOGC/GOMEMLIMIT/GODEBUG/GOMAXPROCS/GOFLAGS/GOWORK were unset; construction used GOMAXPROCS=4. Binary versions and SHA-256 identities are recorded separately. Operational identifiers and credentials are omitted.

The fixture was reset after each native block and returned empty/index-free, effective QPF/TLS/RESP3 verified and all clients/processes closed. The parent completed final infrastructure teardown: the owned container/network and generated TLS/environment/credentials were removed; the preexisting one container and 31 networks were preserved. See [cleanup-proof.json](cleanup-proof.json).

Start from baseline in an isolated checkout, apply candidate.patch there, and build using the normal repository commands. Use the immutable source with the recorded hash, the measured Go toolchain and a dedicated Software fixture prepared using `make -C go redis-up`; independently verify effective 4x QPF before tests. The [reproduction guide](reproduce.md) gives the exact workload and controls. The public wrapper adds ownership-checked cleanup outside trial timing; its five light mock-only cleanup/ownership/CLI checks passed ([checks](repro-wrapper-checks.json)), without a new Redis run. The harness never provisions or tears down Redis. Run the evidence audit without Redis or Go:

```sh
python3 benchmarks/table-modes/batched_arrow_audit.py
```
