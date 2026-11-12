# v0.0.25 performance refresh — November 12, 2026

[Results and methodology](report.md) · [Sanitized measurements](measurements.json) ·
[Recomputed summaries](summary.json) · [Change intent](../../../docs/sdlc/changes/v0025-benchmark-refresh/intent.md)

Run `python3 benchmarks/v0025-refresh/2026-11-12/audit.py` from the repository root
for a standard-library-only arithmetic and coverage audit. This does not rerun SQL.
The complete result validators ran during each original experiment.

The source build is the exact v0.0.25 release. Older mutable-layout benchmark
harnesses require adaptation to current public snapshot APIs; their direct
managed-HASH mutations must not be reused. Native references use a separate
fixed-schema immutable fixture. Read the report before interpreting ratios.

The user excluded the 100M ClickBench rerun. Full-scale and Flight SQL results
remain historical. Operational manifests, endpoints, credentials, raw logs,
binaries, datasets and slide-edit payloads are intentionally not published.
