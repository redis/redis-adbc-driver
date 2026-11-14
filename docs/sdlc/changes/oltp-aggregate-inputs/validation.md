# Validation and review

Date: November 13, 2026 (America/Los_Angeles). Author/reviewer: Codex.
[Intent](intent.md), [spec](spec.md), [plan](plan.md). Human review pending.

- Unit-only `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...`
  passed. Redis integration skips in that command do not prove compatibility.
  Focused runtime eligibility, exact integer bounds, existing grouping contracts,
  direct COUNT source binding, unsupported-input guards and unmanaged predicate
  tests passed separately after the final guards were added.
- Focused Redis Software tests passed on the parent's isolated fixture, after an
  independent active/effective 4x-QPF gate (six Search workers, mnp/32, 4-GiB
  noeviction shard, strict data TLS/RESP3, client GOMAXPROCS=4). Source ca685806
  plus this worktree; operational identities/logs remain private. Ten top-level
  tests passed: three new runtime BIGINT reducer tests, unmanaged aggregate input
  vectors, existing scalar equivalence, aggregate pages, visible-group merging,
  excessive-mask fallback, contiguous-mask proofs and live unmanaged mutations.
- The runtime reducer tests check casted outputs, exact NUMERIC AVG type,
  mixed signs/NULLs, update/delete visibility and zero authoritative HASH-image
  fetches for eligible inputs. Rounded 2^53 groups/extrema, conservative
  cancellation bounds, exact sums beyond int64, cast overflow and an injected
  typed Search group-cap refusal retain their exact fallback behavior.
- Unmanaged checks include exact values above 2^53, true NUL source keys,
  missing fields, application-owned internal-looking fields, live residual
  validation, one reused input vector, consumer errors/cancellation and no
  mutable-index reducer permission. Unsupported expression inputs stay on the
  prior whole-page path.
- Pure CPU/allocation benchmark (Apple M5 Max, darwin/arm64, 300-ms sampling):
  row-map decode/group/reduce 409.3 ns, 752 B, seven allocations per input;
  typed vector 166.0 ns, 16 B, two allocations. This excludes Redis, TLS,
  planning and result construction and is not an end-to-end query speedup.
  Integrated before/after native scenarios are coordinated by the parent task.
- `gofmt` and `git diff --check` passed. All focused live clients exited before
  returning the fixture slot. No new 100M ClickBench/Flex/Flight benchmark ran.

Review passes checked exact integer precision and fallback, retained cursor and
pin ownership, cancellation, live application data preservation, error paths and
actual claims versus measured scope. No actionable findings remain in this
scoped change. Agent review does not constitute human approval.
