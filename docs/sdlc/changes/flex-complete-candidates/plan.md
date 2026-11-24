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
## Cap follow-up verification

The new mocked `78,999` complete-candidate regression failed on the old count
cap (nil candidates, nil error), reproducing the unwanted broad-scan fallback.
The follow-up raises only the constant to 131,072. Tests exercise a complete
78,999-key fetch, exact count-cap admission, existing one-above-count-cap refusal,
and both sides of the independent key-byte boundary. The focused candidate,
page, directory-merge, exact-integer-ordering and validation suite passed
(1.012s) with `GOMAXPROCS=2`, `GOMEMLIMIT=768MiB`, `-count=1 -p 1` and both
Redis endpoint variables unset. Formatting, diff and SDLC local-link checks
passed.
No live query, container operation, benchmark, or external publication is part
of this isolated cap change.

A later combined artifact with this cap change, deferred UNION and streaming
join work still timed out on SF1 Q8 at the unchanged 60-second warm-up deadline,
without a client OOM kill. No Redis timed samples or validated result were
produced. This cap adjustment admits the measured set but does not resolve
issue #594. Native join ordering and delivered key constraints are being
investigated separately. Full suites and TPC-H remain stopped.
