# Plan and evidence

2026-10-21; baseline `96f75c1`; issue #363; author Codex.
See [intent](intent.md), [spec](spec.md). Status: local evidence delivered; deployment/runtime gaps remain.

1. Isolate the refactor in PR #376.
2. Build the pilot harness; smoke-check complete values and schemas before
   repeated larger trials.
3. Run dense narrow/wide/filtered/expression workloads on 100,000 official rows
   with 1/4/8 partitions and 1/4 workers, three repetitions.
4. Run sparse/skewed reduced-data variants and a lowered-cap variant.
5. Inspect prior and current CI job outcomes and actual native validation XML.
6. Publish raw local results, commands, limitations and an explicit remaining
   acceptance-criteria audit. Keep #363 open for unmet deployment requirements.

Initial 10,000-row smoke: 25 trials passed exact values/schema. Q20 returns
zero rows on that prefix; remaining 42 official queries explicitly unsupported.
The first setup attempt exposed case-folding in unquoted DDL vs ingestion field
names; the harness now folds ingest/expected field names without editing SQL.

Task-owned Redis: `redis-adbc-shared-scan-374`, localhost port 16390, Redis
8.6.2/Search 8.6.0. No paid infrastructure. Container is removed after runs.

## Completed work and limits

See the [dated report](../../../../benchmarks/partitions/evidence-2026-10-21/README.md)
for all commands, raw artifacts, hashes, environment and acceptance audit.
All 330 readiness-gated trials passed complete value/schema checks. Initial
ungated sparse failures and failing Arrow outputs are preserved; raw backend
duplicates while indexing and a passing 400-read readiness control are recorded.
#377 remains open for safe runtime handling; this work adds benchmark readiness
gates and public documentation, not a driver guard. Refactor PR #376 is draft.

Audited 16 CI XML reports across current refactor and prior release revisions.
The current refactor's six Linux Software/Cluster jobs passed native suites;
two OSS-single jobs failed before native validation because DuckDB 2.0.0 lacks
the adbc_scanner binary. Analytics CI now pins the preceding green 1.5.6 version;
the existing integration test passed locally without skips. Current installed
macOS wheel full native validation passed: 369 passed, 8 feature skips, 3
subtests, all 7 native partition tests; separate named worker denied CONFIG,
cap contract 137. Cloud/verified TLS/current Linux wheel matrix and larger-scale
evidence remain outstanding. #363 must not auto-close from this PR.

Final local checks: 67 existing ClickBench helper tests completed (3 existing
platform skips), both new scripts compiled with Python 3.13, the native JUnit
gate passed all 7 partition cases, all 18 artifact hashes and source/binary
consistency checks passed, no credential-shaped URIs found in artifacts, and
`git diff --check` passed. Task-owned Redis container removed after testing.
