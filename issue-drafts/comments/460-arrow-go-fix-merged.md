Target: redis/redis-adbc-driver — comment on #460
---
Upstream status update, checked 2026-11-24:

- apache/arrow-go#1598 is closed as completed. The fix is https://github.com/apache/arrow-go/pull/1600, "fix(arrow/cdata): preserve unsliced STRUCT children on export", merged 2026-11-18 as `f13682594a17`.
- It is **not in a release yet**. v18.9.0 (2026-11-16) is the latest release, and the fix commit is 31 commits ahead of that tag. We pin v18.9.0 (`go/go.mod:8`), so the driver still has the defect.

Next steps for this issue:

1. Optional: qualify now against a pseudo-version at `f13682594a17`, using the standalone reproduction above, the strict native lifetime probe and the sliced/nested cross-language cases.
2. Adopt the first Arrow Go release containing apache/arrow-go#1600, re-run this issue's acceptance, then close it.

Three other Arrow Go workarounds can be removed in the same bump; they are tracked together in #647:
- NULL arrays (https://github.com/apache/arrow-go/pull/1550)
- Map NULL keys (https://github.com/apache/arrow-go/pull/1570)
- run-end-encoded validation, which v18.9.0 already fixes (https://github.com/apache/arrow-go/pull/1421)
