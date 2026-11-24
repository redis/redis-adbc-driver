# Plan and verification

Date: 2026-11-23. Source: `07b24edb`. Status: implementation candidate.
See [intent](intent.md) and [specification](spec.md).

1. Attach the query-only filter after exact-directory winner merge; leave
   compaction/publication scanners untouched.
2. Validate control HASHes, bracket bounded per-source Search with fresh index
   proofs, preserve order, refill empty pages, and keep original residuals.
3. Retain compiled source queries in Flex scalar aggregate directory workers.
4. Exercise pure transport mocks for visibility, completeness, cancellation,
   ACL/transport errors, source identity, field mapping, and conservative fallback.
5. Parent investigator reviews/integrates the candidate, then validates selected
   TPC-DS failures on the backed-up Redis Software fixture with verified 4x QPF.
   Full suites and TPC-H remain stopped under the user's current instruction.

Evidence: `env -u REDIS_URI GOMAXPROCS=2 go test -count=1 -p 1 -run
'^(TestSnapshotPage|TestSnapshotDirectoryMerge|TestSnapshotDeltaDirectoryUsesExactIntegerOrdering|TestSnapshotValidationBatch)' .`
passed from `go/` (0.724s test runtime). The 10 new top-level unit tests
include pure mocked transport only. `gofmt` and `git diff --check` passed.
Review by the implementation agent found no remaining actionable issue;
independent review and native server validation remain pending.

No Redis or SQL execution, Docker
operations, performance measurements, publication, or issue-status changes were
performed by this change's implementation agent. The unresolved live-validation
and independent review requirements prevent claiming a benchmark fix today.
