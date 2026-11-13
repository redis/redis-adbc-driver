# Validation

Date: 2026-11-12. Benchmark and API-control validation complete.
Presentation update/visual verification is tracked separately below.

- Exact release build and CLI: `make -C go build adbc-cli VERSION=v0.0.26`;
  source `5c9d583a567e1f6891e6d6ef8703da6189957a04`, GetInfo verified.
- Dedicated fixture from `make -C go redis-up`, Software 8.2.6-25 / Redis 8.8.0 /
  Search 8.8.8, RAM, one shard, active 4x QPF, six Search workers, mnp routing,
  32 connections, noeviction, TLS/RESP3. Effective configuration checked per case.
- 84/84 read records passed complete typed value/multiplicity validation.
- 20/24 write records completed and validated. Four managed four-writer SQL
  records returned 40001; all visible partial rows validated, no successful
  latency claimed. No retry or excluded measured attempt.
- 12 barrier-synchronized API controls: both v0.0.25 and v0.0.26 return one SQL
  success/three conflicts per trial, while all four Arrow appends succeed.
  All visible rows match acknowledged writes, with zero retries.
- Unmanaged Arrow ingestion explicitly attempted and refused as documented.
- `python3 benchmarks/table-modes/audit.py`: passes trial coverage, failure
  accounting, configuration, controls and complete summary arithmetic.
- Private Python environment: `python -m unittest discover -s benchmarks/table-modes
  -p 'test_*.py'`: two tests passed. These cover serializable worker failures,
  endpoint redaction and no automatic replay after the observed exception
  transport problem. No driver behavior changed; full Go suite not needed.
- `git diff --check`: passed.

See the [report](../../../../benchmarks/table-modes/2026-11-12/report.md) for
clock boundaries, environment, medians and interpretation limits. No 100M run,
Flex performance claim or NativeClient benchmark. Existing supporting v0.0.25
results remain labeled by their actual version.

Presentation comparison slides and notes: update/visual QA in progress.
Task-owned fixture cleanup: pending completion of presentation validation.
Operational identifiers, credentials, raw logs and slide payloads remain private.

See [intent](intent.md), [spec](spec.md) and [plan](plan.md).
