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
