# Validation and outstanding work

Date: 2026-11-17. Requestor: Albert Wong. See [intent](intent.md),
[spec](spec.md) and [plan](plan.md). Production fixes are in `6e17a91f`;
performance windows freeze the earlier `3671fca7` in both caller variants.
No timing is relabeled as evidence for later code. This is an agent self-review,
not human approval or authorization to merge.

## Delivered and remaining scope

| Issue | Delivered | Remaining |
|---|---|---|
| #399 | [Proposed ownership/deletion contract](../../../unmanaged-arrow-replace-proposal.md), per Albert's explicit proposal-only decision | Contract review; runtime replace remains unsupported |
| #444 | Fresh metadata proof bug reproduced and fixed without new commands/cache; foreground diagnostic retained | No accepted latency candidate; native ≥10% read gate not established for a driver change |
| #445 | Foreground write/maintenance observations and complete native mutation controls retained | No publication optimization; sustained/backlog/tail/independent-target acceptance remains |
| #446 | [Owner inventory](../../../../benchmarks/crud-execution/results/2026-11-17/ownership.md), paired Go/C BindStream reference correction and callers migrated | Checkptr abort remains before ownership checks; ASAN/UBSAN and complete production lifetime/memory gates remain; all forced GC calls retained |
| #447 | Public executemany(sql, None) caller example, matched same-binary harness/auditor, native parameter/error/final-state checks, separate actual callback-stage diagnostics | Numerical gate met but strong raw-control/order drift makes causal performance conclusion inconclusive; actual ACL, cancellation/Close and stronger lifecycle/control acceptance remain |

All issues remain open; do not use closing keywords for this investigation.

## Correctness and builds

- Regression before metadata fix: both joined missing/EOF bucket cases exposed a
  descriptor with nil error. After correction, every proof reply is checked and
  only a direct native missing entry may establish absence; uncertain outcomes
  do not fetch an object or retain a checked bucket. Unknown pipeline failures
  take precedence over populated native replies. No storage format/new command.
- Regression against archived baseline Go source: both new BindStream ownership
  tests failed (`refs=1` instead of retained 2, and refusal consumed caller ref).
  Corrected code retains before rebind/clear, supports nil unbind and preserves
  caller ownership on refusal. The bridge releases its imported reference on
  success/error. Caller migration follows the current Go ADBC contract.
- `cd go && env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...`:
  unit pass; integration skips expected without URIs, not compatibility proof.
- Native Linux shared driver and CLI builds passed at both frozen baseline and
  corrected source, using Makefile version flags. Corrected normal C/PyArrow
  stub ownership probe passed all seven groups with zero checked Arrow bytes.
- Owned verified Software Linux run: `go test -count=1 -p 1 -race -v -timeout 8m
  -run <selection in checks.json> .`: **102 top-level tests passed, zero skips**,
  27.632 seconds after compilation. Includes metadata corruption/unknown errors,
  reader/binding/NULL/type/provenance, unmanaged append/create/refusals, callback
  input errors, lost replies, cancellation, Substrait and managed replace coverage.
- A broader selection stalled at `TestSQLDropDuringIngest` on macOS and Linux;
  private SIGQUIT stacks were captured and both stopped. The exact test then
  timed out at 60 seconds on unchanged baseline `3671fca7` in Linux too, waiting
  for a Redis pipeline reply during nested competing SQL. This remains an
  explicit failed baseline/environment check, not a new skip or passing suite.
  The 102-test passing selection above excludes this reproduced blocked case.
- `python3 -m unittest discover -s benchmarks/crud-execution -p 'test_*.py'`:
  18 tests passed, covering caller selection/no replay, same-native-build
  enforcement, six independent gates and retention/rejection of evidence.
- Python compile, shell syntax, whitespace and changed Markdown link checks
  passed. No unsupported server or ClickBench work was run.

[Checks and explicit failed baseline](../../../../benchmarks/crud-execution/results/2026-11-17/checks.json)
and [FFI safety identities](../../../../benchmarks/crud-execution/results/2026-11-17/ffi-safety.json)
retain sanitized evidence. Raw logs, environment, certificates and manifests stay
private. The unchanged-policy race probe aborts in createHandle before BindStream;
checkptr was not disabled. Passing Go race tests do not turn that C-ABI gate green.

## Native caller comparison

The installed manager is pinned at 1.13.0. Its public executemany(sql, None)
clears prior results and calls execute_update through its original cancellation
callback. None executes once; an empty sequence executes zero times. Both callers
retain their real preparation behavior, including synchronous executemany prepare.
No wrapper retries, additional closes, SQL classification or upstream API fork.

The four complete ABBA windows passed strict 672-record/42-case auditing. The
predeclared ≥10% pooled one-client mutation p50 gate passed in all six cases.
Strong raw-control drift, nearly unchanged reverse point reads, a slower reverse
managed INSERT and mixed tails prevent an accepted causal speedup claim. All raw
windows/controls/pairs/failures remain in the
[report](../../../../benchmarks/crud-execution/results/2026-11-17/README.md).
No window was rerun or discarded. An initial offline audit rejected a truncated
source argument before output; it was corrected using the full Git identity.

Native correctness screens passed both callers/modes at baseline and corrected
source. They cover affected counts, parameters, exact int64/NULL/NUL, no-op writes,
prior SELECT cleanup, complete SQL states, matching status/SQLSTATE and independent
unmanaged HASH keys/values. Separate 48-trial diagnostic runs per caller retain
1,536 actual operation observations each; normal execute creates 32 result
iterators and closes 31 preceding results per measured one-client trial, while
executemany invokes 32 execute_update callbacks without those result iterators.
No extra callbacks are invoked. Final cursor/connection teardown remains outside
operation clocks as in the original harness; this is not complete lifecycle timing.
Nested stage spans overlap and are not additive GC/server-CPU attribution.

The earlier macOS Go command-count diagnostic overlapped builds and lacks a frozen
whole-test-binary attestation. It is labeled diagnostic only, separate from native
Linux windows; do not use its elapsed numbers for performance acceptance.

## Fixture ownership and review

The first default setup call reached a pre-existing Compose project from another
checkout and updated its proxy certificate before failing QPF configuration.
No SQL tests ran there. The original certificate and matching key were recovered
from a pre-task configuration snapshot, restored via the management API and
verified. The private recovery snapshot/extracted material was deleted afterward.
Only restoration proof is retained privately. No pre-existing fixture data or
resource was removed.

The setup script now refuses another checkout's/unverifiable Compose owner before
creating certificates or changing database policy. Actual default reuse refused
with no setup; the dedicated owned project passed setup and effective 4× QPF.
This safety guard prevents the observed cross-worktree setup side effect.

All live checks/timing afterward used the separately owned Software fixture,
trusted TLS/RESP3, noeviction, active scaling factor 4, six workers and mnp/32.
Go protocol checks used three shards; native windows used one 4-GiB shard.
Identifiers, credentials and logs remain private; public provenance gives OS/route,
source/build hashes and settings only. [Cleanup proof](../../../../benchmarks/crud-execution/results/2026-11-17/cleanup.json)
confirms the empty/index-free endpoint, owned container/network/build-volume and
client-image-tag removal, preservation/restoration of the pre-existing fixture,
and removal of generated local test trust/environment files.

Self-review covered correctness/lifetimes, source/metadata ownership, SQLSTATE and
unknown-outcome precedence, no input replay, bounded proofs, trusted TLS, and
performance provenance. Material limits are the reproduced baseline DDL race,
C-ABI checkptr/sanitizer gates, control drift, and Go callers previously relying on
donation needing to release their caller reference. Human review remains pending.
