# Publication plan and review

2026-10-22. See [intent](intent.md), [spec](spec.md), and the
[archive](../../../../benchmarks/transfer-comparison/README.md).

1. Start a separate branch from merged main and inventory the remaining files.
2. Preserve historical source and raw data; relocate the old Go profiler to
   its dated archive and label both reports as historical.
3. Verify archive identities, JSON, summaries, Python entry points, historical
   Go compilation and local links; record results in `validation.json`.
4. Review the diff, commit the archive and create a PR for maintainer review.

The publication preserves all numerical evidence. Production Go code and
current protocol defaults are unchanged. Historical reports retain their
original recommendations; archive notes explicitly date those statements.

Completed publication checks are recorded in [validation](validation.json):
eight Python scripts parse, six CLI help/import checks pass, all 30 summaries
recompute from 120 recorded executions, eight stage records match their log,
both pprof files parse, and 32 original files are byte-identical. Only the two
reports receive explanatory edits. The profiler compiles against its recorded
revision and skips as expected without the opt-in environment variable; no
Redis performance run or current production regression suite was executed.

Codex performed the three REVIEW.md passes: the diagnostic is isolated from
production and tied to historical source; fixture ownership and local dummy
credentials are documented; all summary statistics and measured-script hashes
match the preserved evidence. No actionable publication finding remains.
This is agent review, not independent human approval. No merge is requested.

Publication: [PR #452](https://github.com/alberttwong/redis-adbc-driver/pull/452).
