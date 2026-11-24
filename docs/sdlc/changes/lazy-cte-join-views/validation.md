# Validation and review

2026-11-23; base `07b24edb3c782037f48670f6cc499d66256a59e0`. See
[intent](intent.md), [spec](spec.md), [plan](plan.md).

Before the implementation, the structural regression
`TestJoinViewsInitiallyRetainNoQualifiedRows` failed because an unscanned alias
already retained a complete qualified copy of the shared relation. It passes
with metadata-only views and per-scan qualification.

Focused pure unit command, run from `go/`:

```sh
env -u REDIS_URI -u REDIS_SNAPSHOT_URI GOMAXPROCS=2 GOMEMLIMIT=768MiB \
  go test -count=1 -p 1 \
  -run '^TestJoinViews|^TestJoinViewScan|^TestGroupedJoin|^TestWholeRow|^TestQualifiedStars|^TestJoinReads$|^TestNeverTrue$|^TestJoinCommon' .
```

The selection passes. New tests establish the structural ownership invariant,
real base-row costing, matched-map lifetime after the full view owner is
cleared, separate physical-key/logical-provenance alignment, and mutable
presence-tag isolation. Error cleanup and cancellation before and during a
partial copy retain no view rows. Result regressions preserve duplicate/NULL
semantics across self aliases, INNER/LEFT/FULL, empty inputs, lateral and isolated
joins, and preserve an arithmetic-error result. Existing grouped/whole-row,
qualified-star, join-read and common-condition tests also pass.

Review: `runJoin` is the only production caller of `joinItem.view`. Initial
emptiness and costing inspect `it.base`, so metadata-only view rows cannot make
a nonempty input appear empty. Needed-column computation and predicate
planning are unchanged. Lateral items use `lateralJoin` with the base item and
do not consume the view's row copy. The per-scan helper copies source rows in their existing order and preserves
every field, then releases only its private input slice on
all returns. Returned matches remain owned maps; it does not clear their
contents or mutate the shared CTE. Native row scans retain the original `scan`
call. No new predicate evaluation or type coercion is introduced.

No Redis URI, live SQL, containers, benchmark query or complete suite was used
for this implementation. No SF1 speedup, Q4 timeout resolution, general join
memory limit or spill behavior is claimed. Root-controlled live comparison on
the preserved fixture remains required after managed Flex filter integration.
Human review/merge approval is pending.

## Combined live follow-up

A later combined artifact (`c9c56f06`, also containing a managed Flex page
filter) was tested on unchanged TPC-DS SF1 Q4 and Q5 with Redis Software Flex,
verified 4× QPF, and a separate 8 GiB client. Both isolated Redis workers were
killed by the client memory limit during warm-up (exit -9, one observed cgroup
OOM kill per case). Their coordinators survived. No validated Redis latency
was produced. This optimization is therefore a tested removal of eager alias
copies, **not a resolution of Q4/Q5 or a complete memory bound**. The remaining
fact scans and join intermediates need separate work. Full suites and TPC-H
remained stopped. See issues #590 and #591 for current evidence.

Independent source review checked the metadata-only view consumers, base
cardinality/emptiness reads, owned-map lifetime, cancellation cleanup, and
unchanged native scan dispatch; no actionable correctness finding remains.
Human maintainer review is still pending.
