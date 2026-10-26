# Incremental-read benchmark intent

2026-10-22. Requested by Albert Wong: test redis-py incremental reads and
whether four ADBC readers can use the same explicit-range pattern. PR creation
was explicitly requested. Author: Codex. Status: measured evidence complete;
maintainer review pending. See [spec](spec.md), [plan](plan.md), and
[validation](validation.json).

The earlier #422 wide-transfer comparison used different native and ADBC Arrow
batch sizes, so its first-batch timings did not establish an equal-row-count
comparison. Measure first normalized Arrow batch and complete typed transfer
with matching output row ceilings, using the same pinned driver and fixture.
Compare one reader and four readers; isolate externally supplied SQL ranges
from public ADBC partition planning/reading.

Scope: a standalone benchmark runner, raw trials, reproducibility metadata and
an evidence report. No production driver change, default tuning, removal of
readiness checks, current-main speed claim, or general SQL/client ranking.
The fixture is immutable with known dense IDs and non-null fields.

The measurement checkout was `c5d09a3`; the publication branch starts at
`f3f74e7`. The measured library is the #422 binary, source-equivalent to
`7a42b2d`, with its exact SHA-256 in the
[report](../../../../benchmarks/incremental-read/2026-10-22/README.md).
Publication: [PR #451](https://github.com/alberttwong/redis-adbc-driver/pull/451). No merge approval is inferred.
