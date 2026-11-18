# Specification

Date: 2026-11-18. Requestor: Albert Wong. Source: 06302c39bd4cb5f8a4e0a1bfc978efc2d5d072ff. Status: implementation.

Run three ordinary Go shards and four snapshot race shards per selected architecture. Each owns a Software fixture verified for active 4x QPF by `make -C go redis-up` and the setup execution wrapper. The ordinary suite retains all runnable tests, examples and fuzz seeds from `go test ./...`; the snapshot suite retains the previous top-level `-run Snapshot` selection with `-race`. Each shard keeps packages sequential, disables result caching and retains the 30-minute package timeout. Child-process helpers remain compiled into each package binary and can select themselves independently.

Discover names with Go under the same race/build configuration; historical durations only determine placement. Unknown new tests are assigned automatically. Require execution completion for every selected top-level test, including explicit skips. Upload only names, outcomes and timing metadata. The final original Linux check names require all bridge, ordinary shard, native, snapshot shard and integration jobs; audit three/four complete, disjoint, consistent shard reports per architecture. A failed, cancelled, skipped or incomplete job must fail the gate.

Run C bridge race/checkptr/cgocheck2 independently. Native Python/RBAC/TLS validation starts without waiting for Go tests. Preserve full diff-based architecture selection and nightly/release/manual triggers. Additional concurrency trades runner minutes for shorter wall time. Do not change production Go code, test deadlines or test assertions.

See [intent](intent.md), [plan](plan.md) and [validation](validation.md).
