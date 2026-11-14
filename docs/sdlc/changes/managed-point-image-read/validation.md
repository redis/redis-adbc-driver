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
