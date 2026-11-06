# Snapshot delivery boundaries

Status: agreed with the user on 2026-11-05. This separates deliverables; it does
not claim that the broader feature/performance objective is complete.

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

Core stabilization remains unfinished. One post-promotion RDB run reported an
incomplete metadata bucket after compaction publication; later passes do not
explain it. Current-source hosted checks must finish and be inspected. Existing
comparative benchmarks retain their exact revisions and measured gaps, including
the approximately 6.5x mutable sustained-write throughput advantage. The delivery
split does not convert that gap into performance parity or erase remaining
optimization and comparative-measurement obligations.
