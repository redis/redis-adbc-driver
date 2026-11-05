# Plan, evidence and review

Date: 2026-10-31. Base `006f09950d72d9bea4b4b7295c07f334c5386f61`.
See [intent](intent.md), [specification](spec.md) and
[public policy](../../../arrow-interoperability.md).

1. Re-read live issue checklists and verify merged predecessors #266/#269.
2. Expand shared independent canonical type/storage/IPC/replay fixtures.
3. Add dictionary payload bounds, numeric offset-zone selection, SQL-domain
   decimal/time bounds, named Struct append compatibility for empty/NULL input,
   and consistent extension validation for ordinary/partition parameters.
4. Add revisioned opt-in extension storage normalization and conservative strict
   incremental-query policy; preserve all default fail-closed/fallback behavior.
5. Validate every result outside timing; run focused, native, full regression
   and CI gates. Publish exact scope/evidence before closing issues on merge.

## Environment and focused results

- Local Apple Silicon, Go 1.27.2; Redis Software Redis 8.6.2, one shard,
  noeviction and active QPF 4x verified through `software-setup.sh --exec`.
  Operational artifacts, endpoints, licenses and raw diagnostics remain private.
- `env -u REDIS_URI ... go -C go test -count=1 -p 1 ./...`: all three packages
  passed (root 25.950 seconds before the final test-only additions).
- Focused unit checks passed with `-race`, including explicit physical decimal
  widths/extrema, numeric-scale bounds, Date64/time-range errors, synthetic large
  offsets, dictionary cardinality/payload/NULL/replacement/empty cases and
  extension registry/JSON/ownership/policy-reset checks.
- Verified Software focused `-race` runs passed: canonical storage/replay matrix
  (initial 49 families; dictionary NULL-value family subsequently added), exact
  unsigned SQL semantics and signed-append rejection, strict key-stream and
  UNION lifecycle/late error checks, and extension independent replay/binding
  policy. The combined matrix/stream/extension run passed in 6.147 seconds.
- Native C module `go/validation/tests/test_arrow_acceptance.py`: **71 passed**
  (2.29 seconds). It covers 48 scalar/nested input families, extension fallback
  and malformed semantics, empty/NULL Struct append mismatch, all four decimal
  widths at three scales, large/view/fixed/dictionary ownership and IPC, both
  partition rejection modes, nested indexing and element overflow.
- Python 3.14.8, PyArrow 26.0.0, driver manager 1.13.0, pytest 9.2.0 in an isolated
  temporary environment. Upstream validation resolved revision
  `5db1f44782f4868c5ae9e89d6ac9812ca6e1758c`. Native libraries use the Makefile
  version linker flags. Full-suite and final-CI results are recorded below.

The complete Software Go run passed all three packages: **1,697 passing test
events, 41 skips, no failures**; root package 253.132 seconds, CLI 2.093 seconds,
benchproxy 0.281 seconds. Command: verified runner `go test -json -count=1 -p 1
./...`; raw events remain private. This run predates the final wide-arithmetic
checks/fix described next and is preserved as intermediate evidence.

The final promotion assertion reproduced an additional #201 bug: adding one to
NUMERIC(38,0)'s largest value returned 22003 instead of promoting. Arithmetic
inference had hardcoded precision 38. The fix retains the minimum 38 for small
operands, reserves integer/carry or combined multiplication precision up to 76,
and gives decimal SUM up to 19 extra coefficient digits (int64 count), capped
at 76. Overflow beyond the Arrow capacity still rejects; no float fallback or
silent rounding is introduced. Focused and final regression results follow.

## Streaming measurements

`ARROW_ACCEPTANCE_PROFILE=1 ... software-setup.sh --exec go test -count=1 -p 1
-v -run '^TestKeyStreamingAcceptanceProfile$' .` runs 54 comparisons: three row
counts, three output cardinalities, three trials, two modes with alternating
order. The query is a managed prepared-key scan with a modulo projection; this
does not measure GROUP BY. All schemas, keys and modulo values are independently
checked after timing. Consumer batches are retained until validation, then
released; every trial owns/closes its statement rather than retaining parsed
plans in test-cleanup closures.

Reported memory is exact Go TotalAlloc and sampled live HeapAlloc after first
fill/each batch/final release, including the process/runtime and retained
consumer buffers. It is neither isolated query memory nor peak RSS; samples can
miss between-batch peaks. ReadMemStats instrumentation is included in full-fetch
timing. Setup, expected-schema planning and final value validation are outside
timing; SQL parsing, preparation, decoding and output conversion are included.
No throughput, fewer-copy or constant-memory improvement is claimed. Measurements
come from the closeout working tree over the base above; final source is the PR
diff, not a claimed measurement of unchanged base main.

The exact samples and medians are in [profile evidence](profile.md).

## Review

Self-review follows REVIEW.md and is not human maintainer approval. Review checks
include subtraction-based budget accounting, validation before allocation,
copy/retain ownership, policy re-evaluation after schema discovery, consistent
extension rejection in partition bindings, named schema identity despite NULLs,
overflow and metadata bounds, and no source reconstruction during retained
replay. Default behavior changes reject invalid/out-of-domain inputs and incompatible
Struct schemas, and widen decimal arithmetic/SUM result metadata when declared
operand capacity requires it. Six contextual quoted-numeric expectations now
use their safe 39–49 digit precision rather than the former 38-digit cap. Strict streaming and extension fallback
are explicit statement options; no transport retry, transaction or consistency
feature is added. Remaining actionable findings and final gates must be resolved
before merge/closure.

## Final local regression

After decimal promotion and the final native assertions, the unit suite passed
all three packages (root 26.195 seconds). The complete native C/upstream ADBC
suite passed **477 tests and 3 subtests, with 8 existing skips** in 17.24 seconds.
The skips are unsupported metadata capabilities and transactions, not Arrow
acceptance exceptions. The verified Software Go regression and PR CI follow.
