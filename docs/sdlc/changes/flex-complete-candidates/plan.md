# Plan and verification

Date: 2026-11-23. Base: `07b24edb`. Status: implementation candidate.
See [intent](intent.md), [specification](spec.md).

1. Probe bounded source cardinalities, completely fetch eligible candidates,
   prove own exact-directory membership, and prepare sorted immutable heads.
2. Reuse candidate visibility merge, adding strict Flex zero-score/lease checks.
3. Switch query scans to complete candidates or original directory fallback.
4. Exercise pure transport regressions and preserve previous focused regressions.
5. Parent reviews/integrates, then validates selected queries on backed-up Redis
   Software with verified active 4x QPF. Full benchmarks and TPC-H remain stopped.

Final verification passed from `go/`:
`env -u REDIS_URI GOMAXPROCS=2 go test -count=1 -p 1 -run
'^(TestFlexComplete|TestSnapshotPage|TestSnapshotDirectoryMerge|TestSnapshotDeltaDirectoryUsesExactIntegerOrdering|TestSnapshotValidationBatch)' .`
(0.763s test runtime). Ten new complete-candidate top-level tests include the
native-limit fallback and byte-cap cases, alongside the prior page/merge/proof
regressions. Formatting and `git diff --check` passed. Implementation-agent
review found no remaining actionable finding; independent review is pending.

No SQL, real Redis, containers, pushes, external messages, benchmark timings,
or issue status changes were performed. No live performance or correctness
claim follows from mocked transport tests.

Reviewable Flex-only implementation: [PR #604](https://github.com/redis/redis-adbc-driver/pull/604).
