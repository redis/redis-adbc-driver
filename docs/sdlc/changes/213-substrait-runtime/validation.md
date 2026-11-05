# Validation: bounded Substrait runtime

Date: 2026-11-04. Baseline: `f4d5b78`. Author: Codex.
[Intent](intent.md), [spec](spec.md), [plan](plan.md), [agent review](review.md).
No maintainer approval or release is implied.

## Environment

macOS ARM64, repository-pinned Go 1.27.3, native cgo library, Python 3.14 and
PyArrow 26.0.1. Dedicated Redis Software, Redis 8.8.0, three shards, strict TLS
with a trusted local CA. `make -C go redis-up` and the script's `--exec` path
verified active 4x QPF, six effective Search workers, mnp routing and 32
connections before integration. The owned fixture verifies its noeviction
policy; the execution path supplies the externally verified
`REDIS_RETAINED_CONTROL_PERSISTENCE=guaranteed` declaration required by retained
storage tests. Credentials, certificates, operational logs and infrastructure
identifiers remain outside this change.

## Results

- Full ordinary Go Software run through
  `bash software-setup.sh --exec go test -json -count=1 -p 1 -timeout 30m ./...`:
  **1,853 pass events, 45 explicit skips, no failed tests/packages**; root
  package 349.904 seconds, all six test-bearing packages passed. Generated
  packages/CUDA have no test files. This complete run is the runtime checkpoint
  before the final wire-integer/privacy rejection follow-ups below.
- Final complete Substrait selection on the verified Software fixture,
  `go test -json -count=1 -race -run '^TestSubstrait' .`:
  **58 passes, zero skips/failures**, no race report. This includes the final
  integer/enum/packed-ordinal overflow and diagnostic-privacy regressions.
- Unit-only package pass, `env -u REDIS_URI go test -json -count=1 -p 1 ./...`:
  **999 passes, 614 expected integration skips**, no failures, all six
  test-bearing packages passed. Later pure admission/privacy regressions are
  included in the final focused race selection; skips do not establish Redis
  compatibility.
- Native library build and complete `make -C go validate`:
  **564 passed, 8 explicit skips, 3 subtests passed**, no failures. This is the
  final rebuilt library, including the wire-integer/privacy follow-ups.
  The four targeted native Substrait tests also passed separately.
- Final decoder fuzzing, `go test -run '^$' -fuzz '^FuzzSubstrait$' -fuzztime=30s .`:
  **1,219,970 executions**, no crash, panic or failing input. Earlier successful
  30-second runs are historical checkpoints, not additional coverage claims.
- Generated descriptor equivalence and pinned source/extension/descriptor SHA256
  checks pass. Every generated Substrait descriptor matches upstream, excluding
  source-location metadata only. Go import mappings preserve descriptor options.
- `gofmt`, Python compilation, regeneration-patch idempotence and
  `git diff --check` pass. All 36 relative links in the runtime/binding/fixture
  records and canonical specification resolve.

## Acceptance observations

Complete Arrow fetches validate independent expected values and compare with
SQL: integers beyond 2^53, positive/negative Decimal(38,9), NULLs, binary bytes,
strings, second/ms/us/ns timestamps before the epoch (naive/UTC), and float
NaN/infinities/signed zero. Both result modes are exercised. Read/filter/project,
direct/emit ordering, duplicate labels, typed NULLs and identity casts retain
physical Arrow types and required/nullable output fields.

Binding cases cover literal schema/table/column names, current/explicit catalog,
owner pg_temp, physical-temp rejection, base-table/view separation, stale schema,
DROP/recreate and eligible application-owned adopted HASHes. Failed setters
preserve previous SQL/plan state; SQL switching clears plan state. Parameter
schema is empty; rejected Bind/BindStream release owned input with checked
allocator accounting. A fully fetched 12,001-row result independently checks
all row IDs/values/NULLs. Cancellation, Close, early release, timeout and retained
consumer buffers are exercised under the race detector.

Malformed/unknown nested fields/enums, duplicate singular/oneof tags, version,
unsupported relations/files/parameters/functions, anchor zero/duplicate/dangling,
wrong output types and schema/ordinal errors reject. Boundary cases cover plan
bytes, literal payload, columns, declarations, 4,096 expressions and 8,192
expanded nodes. Scan budgets count input discarded by residual filters; byte
and deadline boundary cases fail explicitly. Native C tests supply null/nonzero
pointers, invalid addresses with oversized lengths and SIZE_MAX, then execute
prior SQL successfully to prove no copy/dereference or driver poisoning.

Driver-manager GetInfo requested/all-code queries report true and exact
`0.108.0` min/max; metadata and query readers are completely drained/released.
No generic validation-model Substrait flag exists, so explicit tests cover this
contract beside the model's existing SQL/Arrow tests.

## Failed checks corrected during development

The first full Software invocation omitted the required retained-persistence
declaration and failed at that admission gate; it is not passing evidence.
Rerunning through the verified setup execution path passed completely.
The initial native C test had a macOS library search-path error; running from
the built library directory corrected it. Descriptor comparison exposed buf
managed-mode option rewrites; explicit import mappings corrected generation.
Test-harness compilation/error-comparison mistakes were corrected; no production
assertion or skip was weakened to hide a regression.

## Evidence boundaries

This is local macOS ARM64/Redis Software evidence. The ordinary full-suite result
precedes only the final Substrait rejection/privacy follow-ups; those have final
focused/race/fuzz and complete rebuilt-native coverage. No hosted CI, Windows,
Linux native matrix, other Redis deployment, external SQL-to-Substrait compiler
interoperability, performance or release claim is made. These boundaries do not
expand the documented plan subset. The draft PR requires maintainer review and
merge before issue #213 should close.
