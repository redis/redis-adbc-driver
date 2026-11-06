# Deferred snapshot work and preserved code

These follow-ups are outside [minimal core PR #284](core-spec.md), per the user's
request. They are not completion gates for atomic managed-table statement
snapshots, and are not claimed as implemented. Every issue was created with
existing repository labels at creation. #335–#341 use `enhancement`;
the separate recovery investigation #345 uses `bug` and `needs-triage`.

| Follow-up | Scope |
|---|---|
| [#335](https://github.com/redis/redis-adbc-driver/issues/335) | explicit process-resumable imports, SQL and Arrow operations |
| [#336](https://github.com/redis/redis-adbc-driver/issues/336) | reduce foreground metadata, publication and index overhead |
| [#337](https://github.com/redis/redis-adbc-driver/issues/337) | sustained compaction throughput, storage and backpressure parity |
| [#338](https://github.com/redis/redis-adbc-driver/issues/338) | close read-performance gaps and evaluate native typed export |
| [#339](https://github.com/redis/redis-adbc-driver/issues/339) | adopt upstream native read views and retire custom snapshot machinery |
| [#340](https://github.com/redis/redis-adbc-driver/issues/340) | integrate coordinated cross-shard views when upstream supports them |
| [#341](https://github.com/redis/redis-adbc-driver/issues/341) | replace remaining cumulative utility staging quotas with bounded execution |
| [#345](https://github.com/redis/redis-adbc-driver/issues/345) | investigate the retained intermittent post-promotion RDB metadata error; not declared fixed or a fluke |

## Saved code

Remote branch: [codex/snapshot-input-completion](https://github.com/redis/redis-adbc-driver/tree/codex/snapshot-input-completion).
Pinned inventory: [1a641d7f deferred-resume-code.md](https://github.com/redis/redis-adbc-driver/blob/1a641d7f/docs/sdlc/changes/snapshot-parity/deferred-resume-code.md).
The branch is preserved, not opened as another PR or merged into #284.

It includes bounded private capture completion/admission, exact typed/binary
journals, atomic consumption/outcomes, the reserved-row-ID experiment and related
regressions. The earlier capture checkpoint has broad race/native/permission and
Cluster persistence evidence. The final strengthened reservation changes were
compiled but not run as a complete final selection; the inventory distinguishes
those sources. Public process resume and independent retained ownership are
not implemented. Do not infer readiness from the branch's existence.

Existing foreground/read/compaction implementations, tests, design records and
source-specific comparisons remain in the core history and are linked from the
performance issues; no working code was deleted to create these follow-ups.
Private operational scripts, credentials, licenses and raw fixture records were
excluded from the saved branch.

## Upstream dependencies

The [delivery record](delivery-scope.md#upstream-replacement-plan) links the six
Redis/Search requests. Native view adoption, coordinated Cluster views and
read/export follow-ups link those dependencies rather than duplicating them.
Open upstream proposals do not establish availability or justify removing
existing snapshot protections. Per the 2026-11-06 user decision, the unexplained
RDB error is investigated separately in #345. Core safety requirements remain
unchanged: explicit failure without mixed/incorrect data or uncertain replay.
Any evidence of lost acknowledged state or deletion of referenced data remains
a core blocker. The failed attempt, tests and diagnostic code are retained.
