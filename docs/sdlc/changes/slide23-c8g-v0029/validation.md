# Validation and review evidence

Date: November 18, 2026. Status: complete with recorded query limits.
See [intent](intent.md), [spec](spec.md), and [plan](plan.md).

The final ClickBench query library is main `14b4a494`, not the PR head or the
published v0.0.29 binary. The initial full load and separate table-mode refresh
are explicitly attributed to the official v0.0.29 release. No timing or raw
output was relabeled after pulling newer source.

- `python3 benchmarks/table-modes/release-v0029-2026-11-18/audit.py` passed:
  all 324 validated release trials, chart regeneration, build identity, QPF,
  provenance and fixture cleanup. The report hash was refreshed after adding
  the link to the subsequent ClickBench stage; measurement bytes were preserved.
- Both ClickBench `analyze.py` source audits passed: 129 pinned published JSON
  hashes, exact machine type and all 43 query shapes; 118 complete single-host
  CPU hot series. Reported zero timings are retained.
- The full main analyzer with `--run run.json --correctness correctness.json`
  passed: all 129 slot identities/raw hashes, canonical query hash, exact
  driver/build identity, validated hot coverage and the complete comparison
  matrix. Eleven queries passed all three independent DuckDB checks. Sixty
  repeats were explicitly skipped, 33 measured attempts timed out, and three
  Q33 attempts hit the client memory cap. No mismatch or unresolved oracle.
- Separate final checks verified all 43 × 129 matrix rows, all 32 failure/issue
  mappings, raw-record SHA-256 values, and local Markdown links.
- Native Slides text/numeric readback and all 28 rendered slides were inspected.
  One consolidated repair reconciled stale summary wording; the other 25 slide
  renders were byte-identical after that repair. The prior slide deletion and
  seven-slide performance-section counters were preserved.
- Fresh read-only AWS API audit confirmed zero task-owned nonterminated
  instances, volumes or security groups; the task expiry function/schedule,
  instance profile and both roles are absent. Private S3 backup/evidence remain.
- Staged artifact review checked distribution/settings attribution, immutable
  provenance, missing-result policy, source hashes and privacy. Scans found no
  S3 locations, infrastructure identifiers, endpoint credentials, private keys
  or license material. Python caches and native/RDB binaries are excluded.

This PR changes benchmark evidence, analyzers and documentation; it does not
change driver behavior. Driver unit/integration suites were not rerun for this
artifact-only publication. Recorded Software/QPF benchmark executions provide
the measurement evidence. No human review or merge approval is inferred.
