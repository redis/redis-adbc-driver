# Shared read snapshots: plan

Date: 2026-11-10. Source baseline: `85509485`. Status: implementation and local validation complete; awaiting PR review. Requestor: Albert Wong.
Issue: [#365](https://github.com/redis/redis-adbc-driver/issues/365).
Related: [intent](intent.md), [specification](spec.md), [validation](validation.md).

## Ordered work

1. Define native connection options and borrow/release ownership without a new
   durable format (`go/shared_snapshot.go`, `go/snapshot.go`).
2. Bind SQL, schema/parameter discovery, GetObjects, statistics and Substrait;
   preserve internal store/compaction independence and read-only admission.
3. Add catalog/stream/expiry/ownership/quota/process-death regressions and a
   management-backed SQL RBAC revocation regression.
4. Document the exact native Python API, transport/storage boundaries and
   connector enablement prerequisites.
5. Record actual validation and review findings, commit, push and open a PR.

## Deferred acceptance evidence

A published release version and end-to-end Airbyte integration are not produced
by opening this PR. Flight SQL shared snapshots are explicitly unsupported.
Do not close #365 as fully delivered without the release/integration evidence
requested by that issue. No merge or release approval is inferred from code review.
