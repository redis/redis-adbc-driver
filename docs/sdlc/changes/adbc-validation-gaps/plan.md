# Plan

Date: 2026-11-19. Author: Codex. Status: implementation in progress.
See [intent](intent.md), [specification](spec.md), and [validation](validation.md).

1. Constraint subagent owns parsing/persistence/GetObjects declaration metadata
   and lifecycle tests for #535/#536 in an isolated worktree.
2. Table-type subagent audits driverbase/C filtering, proves native behavior and
   owns #537 tests plus its one validation flag.
3. Catalog subagent owns logical registry/composite identity, namespace resolution,
   catalog APIs/lifecycle, Arrow targeting and isolation tests for #538-#541.
4. Root coordinates overlapping Go files, shared metadata semantics and serialized
   leases on the owned verified Software fixture; unit-only work runs in parallel.
5. Review/cherry-pick isolated commits, consolidate validation fixtures/flags, run
   combined native tests and compatibility checks, and address integration defects.
6. Create PR with issue mapping and actual evidence; fix required check failures.
