# Plan

2026-11-24; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Add harness regressions to `benchmarks/olap-release/test_isolation.py`, covering zero-filled results, a zeroed reference, rewritten results, missing digests, scratch damage, digest/fsync, and retention for mismatch, artifact failure and validator timeout. Confirm they fail on unmodified main.
2. Implement artifact digests, verified decoding, `ArtifactError`, `artifact_problem` and `retain_artifacts` in `benchmarks/olap-release/isolation.py`.
3. Document the behavior in `benchmarks/olap-release/README.md`.
4. Run the harness unit tests on Python 3.14 (CI) and locally. Compare 1M-row validation time before and after.
5. Run TPC-H SF1 Q11 and Q2, all three routes, on the retained Software Flex fixture with the changed harness. Record whether every artifact verifies.

No merge, release or human review approval is inferred.
