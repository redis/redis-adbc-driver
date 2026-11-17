# Independent catalog writes: plan

Date: 2026-11-16. Baseline: `225e0056`. Status: implementation and planned validation complete; pending maintainer review.
See [intent](intent.md), [specification](spec.md), [validation](validation.md).

1. Reproduce independent catalog conflicts through four public native ADBC connections.
2. Add leased catalog admission alongside table admission in `go/snapshot.go`
   and `go/snapshot_admission.go`, before any execution/input consumption.
3. Exercise SQL catalog concurrency, Arrow lifecycle and uncertain admission.
4. Run relevant snapshot safety checks, unit suite, native library build and
   native dbt four-thread/parallel functional checks on verified Software 4x QPF.
5. Review lifecycle/ownership, conflict semantics and evidence per `REVIEW.md`.

Tradeoff: catalog statements execute serially, which can constrain CTAS loading
throughput. More granular metadata/member/OID protocols would be a separate
change; merging only name sets leaves other shared dependencies unresolved.
