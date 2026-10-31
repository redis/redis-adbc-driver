# Plan and verification

Date: 2026-10-31. Base: `198de097e88bc48adc99b9dcdbe5ac4fa8341c97`.
See [intent](intent.md) and [specification](spec.md).

1. Inventory each layout and fill valid/NULL/empty/nonzero-slice fixtures with
   independently constructed canonical Arrow expectations.
2. Add safe malformed-dimension, poison-release and IPC stream/file checks.
3. Bound logical expansion before ingestion rows and parameter materialization.
4. Validate metadata reload and independent live/retained readers, including
   retained replay after source truncation; exercise native C bindings.
5. Publish explicit decisions, labeled Map/Union follow-ups and PR evidence.

## Results

- Go 1.27.2, local arm64. Redis Software Redis 8.6.2, one shard, active verified
  4x QPF, noeviction management-API verification through `software-setup.sh --exec`.
  Operational artifacts/identifiers remain private.
- `env -u REDIS_URI ... go -C go test -count=1 -p 1 ./...`: all three packages
  passed. An initial sandboxed run could not open the TLS/proxy tests' localhost
  listeners; rerun with listener access passed without changing assertions.
- `env -u REDIS_URI ... go -C go test -race -count=1 -p 1 -run
  '^TestArrowLayout' .`: passed the inventory, malformed dimensions, copied-value
  lifetime and expansion-budget unit tests. Redis-dependent tests skip in this
  command and are covered by the next command.
- Through the verified Software runner, `go test -race -count=1 -p 1 -v -run
  '^TestArrowLayout(InventoryStorage|ExpansionRejectedBeforeRows)$' .`: passed
  all 12 layout subcases plus ingestion/parameter expansion rejection using both
  Bind and BindStream. Every accepted replay validates the complete result and
  exact schema. Rejected Map/Union fixtures leave no table behind; oversized
  supported input leaves zero rows. Retained fixtures clean their owned state.
- `GOCACHE=... make -C go build`: passed, preserving version linker flags.
- Native C module: Python 3.15.2, PyArrow 26.0.0, ADBC driver manager 1.13.0,
  pytest 9.2.0. **21 passed**, no skips/deselections in the final module run,
  including the 12-family C Bind/BindStream full/sliced/empty inventory and
  decoded-value/payload expansion rejection. Run from an isolated temporary
  environment/connection fixture against the verified endpoint; source tests
  are `go/validation/tests/test_arrow_interop.py`. This is not a run of the full
  upstream ADBC validation suite.

No complete Redis suite pass, performance improvement or bounded-RSS claim is
made. Existing full-suite transport EOFs remain tracked in #265/upstream #776.
This issue's assessment accepts documented Map/Union rejection, with separate
implementation issues #267 and #268; it does not claim those layouts are stored.

## Review

Self-review follows REVIEW.md; it is not human maintainer approval. Checks cover
NULL/empty and slice offsets, owned buffers after poison release, safe preflight
before expansion, overflow-safe subtraction, canonical schemas and replay
independence. Production changes are limited to decoded-input preflight and
validation before ingestion row allocation. No retry/replay, consistency,
transaction or deployment behavior is added. Final static/affected checks and
PR/merge links will be recorded in the issue timeline after completion.

- Affected regressions passed on the verified Software fixture with `-race`:
  `go test -race -count=1 -p 1 -run
  '^(Test.*Ingest.*|Test.*Bind.*|TestSQL.*Array.*|TestSQL.*Record.*|TestArrow.*|TestUnionAllStreamLifecycle)$' .`
  (7.907 seconds). This covers pre-existing ingestion/binding/array/record and
  stream lifecycle behavior after adding whole-record preflight.
- `git diff --check`, Go formatting and relative links in the changed contract
  and new change records passed. Review found no remaining actionable finding
  in the changed behavior; no human approval is inferred from this review.

## CI follow-up

The first PR #269 OSS Cluster run caught a test-oracle mistake: four live
partition cases compared insertion order for a query without ORDER BY. The
logged values/schema were correct but Cluster replay order differed. The test
now requests ORDER BY __rowid for its ordered ordinary-query check, and compares
complete Arrow JSON row multisets for unordered live/retained replay, preserving
duplicates and NULL/empty/nested distinctions and exact schemas. IPC checks
remain order-sensitive. Native inventory queries also explicitly order by rowid.
No production partition behavior or assertion of value/schema equality was
weakened. Updated checks and CI outcomes are recorded in the PR timeline.

The corrected Cluster Go suite passed. Full upstream C validation then exposed
two gaps not covered by the earlier focused module: valid C Null arrays have
zero buffers (the same internal nil-placeholder mismatch as run-end parents),
and a legacy test still expected all Struct ingestion to fail after PR #266.
The validation-only adapter now handles zero-buffer Null arrays recursively
without modifying caller-owned data. A new unit case covers full/sliced/empty
and nested C Null arrays. The legacy test retains anonymous ROW restriction
assertions and adds a named Struct storage round trip, including parent-null
versus all-null children and exact schema checks.

Local full upstream validation passed after these fixes: **405 passed, 8 skipped,
3 subtests passed** (15.73 seconds), through the verified Software runner.
Command: `software-setup.sh --exec env UV_CACHE_DIR=... UV_PROJECT_ENVIRONMENT=...
uv run --project validation pytest -q -ra validation/tests`. The isolated
environment resolved upstream validation revision
`5db1f44782f4868c5ae9e89d6ac9812ca6e1758c`; the supported default test configuration
is `redis`. Skips are the existing unsupported connection/filter/catalog/ingest
and transaction capabilities. No skip or capability flag was broadened.
