# Archive contract

2026-10-22. See [intent](intent.md), [plan](plan.md), and the
[archive](../../../../benchmarks/transfer-comparison/README.md).

- Retain all 34 previously untracked source/evidence files. Move the historical
  Go diagnostic into the #396 archive without modifying its measured source.
- Preserve JSON, logs, profiles and Python snapshot bytes. Record original and
  publication hashes; report edits only add historical context and correct the
  relocated profiler's reproduction path.
- Label the 100K-row fixture, RESP2 native/profile settings, original binary's
  unattested source revision, source-identified #396 run and distinct matrices.
- Check JSON, Python syntax/imports, recorded summary statistics, original
  script hashes and historical Go compilation. Do not describe these checks as
  new benchmark runs or revalidation of all historical output values.

The scripts contain destructive fixture loaders and use local throwaway
credentials. Reproduction requires fresh dedicated servers. No source dataset,
compiled driver, Python cache or unrelated local file belongs in this PR.

Publication: [PR #452](https://github.com/alberttwong/redis-adbc-driver/pull/452).
