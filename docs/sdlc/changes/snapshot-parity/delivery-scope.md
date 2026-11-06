# Snapshot delivery boundaries

Status: agreed with the user on 2026-11-05. This separates deliverables; it does
not claim that the broader feature/performance objective is complete.

The [minimal core specification](core-spec.md) is now the completion boundary
for PR #284, superseding the earlier broad parity requirements for this delivery.

PR #284 delivers and stabilizes managed-table atomic statement snapshots:
visibility-correct reads, atomic publication, pinned readers and shared-generation
live partitions, reference-safe maintenance, existing feature compatibility and
honest platform/Cluster/failover/persistence evidence. Application-owned HASHes
and foreign indexes remain managed copies with explicit refresh. Preserve
no migration, autocommit-only scope, Active-Active exclusion and noeviction.
The separate Flex stopgap is not part of this PR.

Public operation/input resume is a separate follow-up. Its independent retained
ownership, takeover fencing, persisted dependency/index plan, stable row bindings,
explicit native status/resume/abort API, and separate-process recovery proof must
not be inferred from PR #284's private batch checkpoints. The private capture
completion and row-ID reservation development is retained on a separate branch;
it is not part of the published core snapshot revision. See the
[resume design](import-resume-design.md) for the remaining work.

Redis-native reusable point-in-time query read views are requested upstream in
[redis/redis#16180](https://github.com/redis/redis/issues/16180). An upstream read
view might reduce driver storage/index/compaction overhead; it does not itself
implement operation resume. No upstream availability or performance is assumed.

On 2026-11-06 the user approved moving the intermittent post-promotion RDB
metadata error to [#345](https://github.com/redis/redis-adbc-driver/issues/345).
Its causal explanation is no longer a completion gate for #344 / PR #284.
The observed query failed explicitly; the cause remains unknown. Core correctness
still excludes mixed/incorrect results, live fallback, uncertain input replay,
lost acknowledged state and deletion of referenced data. Any evidence of those
defects remains a core blocker. Recovery tests and failed evidence stay intact.
Current-source hosted checks must finish and be inspected. Existing
comparative benchmarks retain their exact revisions and measured gaps, including
the approximately 6.5x mutable sustained-write throughput advantage. The delivery
split does not convert that gap into performance parity or erase remaining
optimization and comparative-measurement obligations.

## Upstream replacement plan

The user subsequently requested reducing both already implemented and remaining
custom work as far as possible. Freeze additional bespoke operation-resume and
performance infrastructure in PR #284; retain the private development branch
without publishing it as part of the core snapshot change. Existing working
snapshot protections remain necessary until a replacement is available and
verified; filing a request alone does not make its feature supported.

| Upstream request | Driver machinery it could replace or avoid |
|---|---|
| [Redis #16180: reusable point-in-time read views](https://github.com/redis/redis/issues/16180) (existing) | Custom immutable row generations, reader retention, version directories, reference accounting and reclamation |
| [Search #12219: view-bound Search and cursors](https://github.com/RediSearch/RediSearch/issues/12219) (new) | Retained index generations, visibility candidate rechecks, reducer masks, index/version compaction |
| [Redis #16184: coordinated cross-shard views](https://github.com/redis/redis/issues/16184) (new) | Driver-specific distributed read coordination and dependence on one-slot managed data placement |
| [Search #12220: index/write-boundary completion barrier](https://github.com/RediSearch/RediSearch/issues/12220) (new) | Repeated readiness polling, exact coverage validation and custom final-index notification workarounds |
| [Redis #16185: bounded mutation outcomes](https://github.com/redis/redis/issues/16185) (new) | Custom mutation identities, outcome journals, partial-failure bookkeeping and uncertain-reply recovery protocol |
| [Redis #16048: typed bounded bulk export](https://github.com/redis/redis/issues/16048) (existing) | Repeated per-cell RESP decoding and parts of the custom analytical transfer pipeline |

Each request states acceptance criteria, limitations and alternatives; the new
requests were checked against existing discussions. The user explicitly allowed
unlabeled upstream submissions because this account cannot assign labels.
The two core requests were submitted through the user's signed-in browser after
API credentials refused creation. All four new issue bodies and open states
were verified through GitHub after submission.

These requests are proposals, not accepted implementation commitments. Do not
publish an unsupported Redis bug claim for the unexplained metadata recovery
failure: it still requires driver-level diagnosis. SQL/Arrow semantics, managed
external-copy refresh, error propagation and native ADBC integration remain
client responsibilities. Native mutation outcomes alone also do not implement
unread-input capture or full process resume.

The minimal immediate deliverable is core statement-snapshot correctness and
stabilization with truthful evidence/limits. Public operation resume, new bespoke
optimizations and cross-shard snapshot coordination are deferred rather than
silently counted as completed. Performance parity is not a core completion claim.

The [deferred-work index](deferred-work.md) records driver follow-ups #335–#341
and the preserved remote resume-prerequisite branch. None is a minimal core
completion gate; final WIP reservation code is explicitly not fully validated.
