# Plan

See [intent](intent.md) and [specification](spec.md).

1. Inspect issue #609 and merged PR #610; retain its direct-Nil-only correction.
2. Add populated-reply regressions for map and positional HASH consumers,
   including direct-Nil controls and independent command failures.
3. Reproduce the old behavior using a temporary Go source overlay, without
   modifying the working production source.
4. Format changed Go files, run standalone unit checks and inspect the scoped
   diff using the repository review policy. Record results below.

## Verification and review

- Pre-fix reproduction: a temporary `go test -overlay` restored the old
  `errors.Is` guard. `TestHashFetchPopulatedRepliesPreserveUncertainErrors`
  failed for wrapped Nil and joined Nil/transport errors in both readers
  (four failing subtests, test exit 1), demonstrating false success.
- Fixed-source focused command: `cd go && env -u REDIS_URI go test -count=1
  -p 1 -run '^(TestHashFetch|TestScalarCohortReviewAmbiguousHashPipelineFailsClosed)' .`
  passed (0.666 s), including the existing parsed-cohort source/cache assertions.
- The broad `go test -count=1 -p 1 ./...` encountered unrelated untracked build
  worktrees/cache contents, including forbidden internal imports and C-source
  setup errors; it was stopped. No build artifacts were changed or removed.
- A tracked-source package enumeration then ran `env -u REDIS_URI go test
  -count=1 -p 1` with explicit package directories. All 11 standalone packages
  passed or had no tests; the root passed in 30.013 s. Explicitly including
  `./pkg` produced a build-constraint setup failure because it requires the
  separate `driverlib` build tag. The aggregate command therefore exited 1.
- The separate shared-library package check, `cd go && env -u REDIS_URI
  go test -count=1 -p 1 -tags driverlib ./pkg`, passed (0.772 s).
- `gofmt` and `git diff --check` passed. Scoped self-review against `REVIEW.md`
  found no actionable issue: tests exercise both callers, preserve diagnostic
  text and direct-Nil values, and retain independent command failures. This is
  not maintainer approval or an audit of other helpers.
- Local change-record links were verified.

No live Redis tests or benchmarks ran. Redis-URI integration tests are skipped
in the standalone pass; those skips do not establish Software compatibility.

## PR preparation verification

On 2026-11-24, copied only this regression and its records into a clean worktree
based on `f6e0bbbe`, leaving unrelated working changes untouched. In that clean
worktree, `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...` passed
(exit 0; root 28.481 s), and `env -u REDIS_URI go test -count=1 -p 1
-tags driverlib ./pkg` passed (0.572 s). This resolves the broad-command
artifact-discovery limitation above for the PR source. Integration skips remain.
`git diff --check` and local change-record link checks also passed.
