# Validation and review

Date: November 14, 2026. Author/reviewer: Codex; human review pending.
[Intent](intent.md), [specification](spec.md), [plan](plan.md).

Parent-owned Redis Software fixture independently verified active/effective 4x
QPF, six Search workers, mnp/32, noeviction and certificate-verified TLS/RESP3
before each live selection. Agents use serial live slots; no new fixture was
provisioned by this agent. Diagnostic runs use GOMAXPROCS=4; concurrent source
compilation can affect wall times. Native final paired measurements are separate.

Baseline production source 0b216478: existing sustained/stage/index-wait profiles
PASS. Fresh final indexes were ready on their first poll; create-to-readiness was
1.77–2.60 ms, so reducing readiness sleeps was not justified. The matching new
1K/four-column diagnostic validates all 96 mutations and complete final states.
Its medians were INSERT 39.934 ms, UPDATE 64.733 ms, DELETE 49.587 ms. These are
instrumented Go times, not Python/ADBC timings or server CPU. Threshold-selected
slowlog was available but contained no admission/Lua event above its threshold;
that absence does not establish zero server cost.

Focused new Software checks PASS (1.726s): exact 9007199254740993 strings,
newest/tombstone/private visibility, no unused older reads, absent rows, wrong
HASH/directory type, missing/mismatched row IDs and missing directories, top-level
ACL fallback, terminal inner failures/unknown replies, argument bounds and
unchanged strict/multi-row paths. Unknown replies use the existing no-retry
wrapper. Additional relevant regressions, candidate profile and coordinator
integration evidence will be attached before final claims.

## Candidate evidence

Production candidate `6af289dd5edd205d4d69fe65a5839a4b1109ee72` completes the
identical 96-mutation diagnostic with all final states valid. Sanitized complete
[before/after evidence](../../../../benchmarks/managed-point-images/2026-11-14/report.md)
retains the slower INSERT case and actual source/harness hashes. Client exchange
counts are distinct from server commands executed inside Lua. These observations
remain diagnostic until the coordinator's isolated paired native measurements.

A further relevant Software selection passes 33 top-level tests, zero skips or
failures, 8.983s: versioned DML/row-directory corruption, private index notification
and unknown outcomes, independent publication/concurrent writers, Arrow atomicity
and modes, strict direct partitions and the new point proof/permission cases.
All processes and clients exited before the shared live slot was released.

Unit-only `env -u REDIS_URI -u REDIS_SNAPSHOT_URI GOMAXPROCS=4 go test -count=1
-p 1 ./...`: PASS in all Go packages; native integration cases skip without
endpoints. `python3 benchmarks/managed-point-images/audit.py`: PASS, covering
96+96 retained diagnostic observations, exact coverage, source/input hashes and
comparison arithmetic. `gofmt` and `git diff --check`: PASS. No new live Redis
work ran during these offline checks.
