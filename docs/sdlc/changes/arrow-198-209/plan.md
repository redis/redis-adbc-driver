# Plan and verification evidence

Date: 2026-10-30. Base `de3404298309d46d17a8dce0518806c434454cda`.
See [intent](intent.md), [specification](spec.md), and
[public contract](../../../arrow-interoperability.md).

1. Inspect current issues/source; move the initially stale detached worktree to
   a task branch based on available current origin/main.
2. Implement canonical input normalization, named/nested storage, safe streaming
   eligibility, explicit output schemas/readers and partition guards.
3. Add unit/Redis/C-boundary tests, update previous unsupported/range assertions.
4. Review ownership, precision, nested storage and documentation; record actual
   checks and preserve remaining acceptance gaps.

## Environment and checks

- Local Apple Silicon, Go 1.27.2. `make -C go redis-up` verified Redis Software
  Redis 8.6.2, active QPF 4x before testing. No infrastructure identifiers or
  credentials are included here.
- Unit-only suite: `env -u REDIS_URI GOCACHE=... go -C go test -count=1 -p 1 ./...`
  passed all three packages. Redis-dependent cases skip without REDIS_URI.
  Initial sandboxed run could not open localhost listeners; rerun with socket
  access passed. This was an environment restriction, not a weakened assertion.
- Focused Redis tests: `TestArrowNormalizedStorage`, `TestArrowResultLayouts`,
  `TestKeySourceStreaming`, `TestUnionAllStream*`, duration boundaries,
  named/nested storage and physical conversion tests passed. They fully fetch
  values, compare schemas and exercise IPC, create/append/BindStream and lifecycle.
- Affected regressions: SQL record/error-state, ingest/bind lifetime, array,
  temporal and retained-partition filters passed after adapting the uint64 range
  assertion to the target INTEGER column and enabling named struct parameters.
- `GOCACHE=... make -C go build` passed with the Makefile version linker flag.
- Native C tests: Python 3.15.2, PyArrow 26.0.0, adbc-driver-manager 1.13.0,
  pytest 9.2.0 in an isolated temporary environment. The initial six cases passed
  using a local DBAPI connection fixture; the repository test module is
  `go/validation/tests/test_arrow_interop.py`. No full upstream validation-suite
  result is claimed. Additional cases/final reruns are recorded below.

## Remaining issue acceptance

The issues remain open. No throughput/first-batch/RSS improvement is claimed.
Run comparative first-batch/full-fetch and cardinality/allocation benchmarks
before performance claims. Extend coverage to all deployment topologies, extreme
buffer limits, and independent partition replay combinations. General blocking
operator resource limits/spill, stored unsigned physical identity, persistent
input layouts, true Map/Union storage and extension registration remain outside
this implementation. The README retains partial/unsupported classifications.

## Review

Self-review follows REVIEW.md; it is not maintainer approval. Correctness checks
include binary-safe nested cells (arbitrary bytes must use base64 JSON cells),
exact numeric storage, rejection before partition publication, matching output
schemas and retained batch ownership. No merge/release action is authorized by
these records.

## Final focused evidence

- Eight native C cases passed, including sliced FixedSizeList/ListView/run-end
  input and canonical UUID/custom extension rejection. A valid C run-end parent
  has zero buffers; Arrow Go validation expects a nil AlwaysNull slot. A
  validation-only retained view now adapts that slot while preserving all run,
  child, offset and length checks. Invalid run boundaries still fail.
- Canonical nested/NUMERIC values passed portable partition replay with full
  value and schema equality. Selected physical layouts remain rejected for
  both partition modes.
- Added a binary-poison lifetime test, dictionary-cardinality bound test, type
  depth/precision/zone rejection tests, and fixed-binary amplification test.
- A one-trial key-predicate profile fully fetched results, then independently
  checked every value/duplicate outside the timed interval. Consumer batches
  were retained; allocations are Go TotalAlloc, **not** peak RSS. Local results:

| Rows | Streaming | First batch ms | Full fetch ms | First allocated bytes | Full allocated bytes |
|---:|---|---:|---:|---:|---:|
| 128 | false | 31.03 | 31.09 | 861464 | 861464 |
| 128 | true | 29.26 | 30.18 | 827976 | 862640 |
| 2048 | false | 55.17 | 55.20 | 20113224 | 20113240 |
| 2048 | true | 56.29 | 57.33 | 20038656 | 20151568 |
| 8192 | false | 60.94 | 60.97 | 29003352 | 29003368 |
| 8192 | true | 60.87 | 62.88 | 28872472 | 29226920 |

Command: `ARROW_INTEROP_PROFILE=1 REDIS_URI=... GOCACHE=... go -C go test
-count=1 -p 1 -v -run '^TestArrowKeyStreamingProfile$' .`. Query parsing,
preparation, client decoding and output buffers are included. These local single
samples show no reliable speedup and do not establish a bounded-memory claim.

- The focused Redis suites passed with Go's race detector, including concurrent
  Next/Cancel and UNION child error/release cases. The final library builds;
  all eight native C driver-manager cases passed after the run-end adapter.
- Fixed binary selection is limited to 16 MiB per field and converted in chunks
  targeting 16 MiB of fixed payload per batch (a single wide row may exceed
  that). This prevents a large source NULL batch from multiplying into one
  enormous fixed-width allocation; retained batches and source data remain
  outside that payload target.
- `git diff --check` and relative links in the new change records/contract passed.

## Full-suite limitation

The final `REDIS_URI=... go -C go test -count=1 -p 1 ./...` did **not** pass:
main package failed after 214.6 seconds; CLI/proxy packages passed. Failures were
`TestSQLAlterTypeLostLease` (abandoned-rewrite timing assertion), retained API
publication/capture tests (the fixture eviction policy does not preserve their
control ledgers), `TestSQLInformationSchema` (leftover `it_unklit` table), and
UPDATE/MERGE fault-proxy subtests (connection EOF). No assertions or skips were
weakened and no eviction-policy guarantee was fabricated.

An unchanged `origin/main` snapshot in a temporary directory ran the four
representative tests in isolation. The retained publication policy error
reproduced on unchanged main; the lease, information-schema and fault-proxy
cases passed that isolated baseline run. This establishes a baseline policy
limitation; it does not establish that every full-suite timing failure is
unrelated to this patch. Focused rerun results are recorded below.

- The changed branch's isolated rerun of `TestSQLAlterTypeLostLease`,
  `TestSQLInformationSchema` and `TestSQLTruncateOrDropDuringUpdate` passed
  (3.77 seconds). These remain intermittent full-suite failures, not a clean
  complete-suite result. The retained policy guard remains a known fixture
  limitation reproduced on the base revision.

## Follow-up: fix full-suite failures

Requested by the user after the initial handoff. Preserve all earlier evidence;
this section supersedes its unresolved status only when the checks below pass.

- Verified the Software management API reports noeviction, while its data proxy
  returns an empty successful CONFIG map. Added `make -C go test-software` and
  `software-setup.sh --exec` so the owned endpoint's API topology/policy and
  effective 4x QPF gates precede the explicit retained-control declaration.
  Missing policy introspection now returns NotImplemented with the declaration
  guidance; an actual unsafe policy still returns InvalidState.
- The lost-lease test assumed context cancellation always prevents owner cleanup
  from completing. It now injects that specific cancelled state-read failure on
  the owner's maintenance client and asserts the injection occurred. The other
  connection must still recover the recorded abandoned rewrite, preserving all
  original rollback/value/cleanup assertions. No production lease logic changed.
- The information-schema test now selects its owned fixture names, with an
  unrelated test table present, instead of assuming every `it_%` table belongs
  to it. Existing names/types/column/view assertions remain exact.
- A separate disposable Software fixture was created to avoid cross-session
  test-name and recovery collisions. Management API setup verified noeviction
  and active 4x QPF before every test run. No global policy was changed or
  inferred from the empty data-plane reply.
- The four previously failing representative cases passed ten consecutive
  repetitions (40 top-level cases, with nested update/truncate subcases) in
  30.90 seconds. Full-suite and unit results follow.

## Follow-up results and EOF tracking (2026-10-31)

- Unit suite: all three packages passed. `scripts/test_software_setup_exec.py`:
  all three cases passed, including refusal to execute on an unsafe API policy.
- Isolated full Software run through `make -C go test-software TEST_ARGS=-json`:
  **1,415 passing, 40 skipped, four failing test/subtest events**; root package
  209.037 seconds. CLI and benchproxy packages passed. The policy, lost-lease,
  and scoped information-schema fixes passed; remaining failures were EOFs in
  `TestSQLParallelAggregates`, `TestSQLParameterSchema`,
  `TestBalancedPartitionReads` and `TestSQLCatalogOIDLifecycle`.
- Opened [#265](https://github.com/redis/redis-adbc-driver/issues/265) with the
  existing bug label and fresh single-shard reproduction, linked to #217.
- The exact upstream report already exists as
  [Docker Desktop #776](https://github.com/docker/desktop-feedback/issues/776).
  Added [fresh versions/results](https://github.com/docker/desktop-feedback/issues/776#issuecomment-6540452665) there instead of submitting a duplicate.
- Private command-name-only handshake trace: sequential 3,000-client conditions
  yielded zero failures with auto maintenance notifications and 466 with them
  disabled. Each failing trace contained only HELLO. This was not randomized or
  rate-matched and does not establish a notification-related cause; production
  notification settings and command-retry behavior remain unchanged.
- Later read-only controls on the same server after the full suite passed all
  3,000 clients each: container loopback (0.64 s), host numeric IPv4 loopback
  unpaced (3.59 s), host localhost unpaced (4.39 s), host numeric IPv4 loopback
  with a 2 ms/client pause (13.16 s). These passing probes do not establish a
  fix or isolate a Docker defect. Raw diagnostic files stay outside the repo;
  temporary probe code was removed from the source tree.

The disposable Software container/network created for the isolated follow-up
was removed after collecting results. The pre-existing shared fixture was left
running. Unit/shell checks and `git diff --check` pass; full-suite EOFs remain
open in #265 and the upstream Docker discussion. No causal attribution or
complete full-suite pass is claimed.
