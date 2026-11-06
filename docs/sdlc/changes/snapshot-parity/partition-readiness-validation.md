# Pinned partition scan readiness

Date: 2026-11-04. Parent f009584. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The ordinary continuation reaches TestBoundedScanRejectsBuildingIndex and fails
inside sharedScanPlan because its internal catalog read has no statement pin.
Readiness tests now plan and execute under one actual inspection snapshot. Hooks
are installed before the client is shared with its heartbeat; real immutable
index validation runs before readiness injection is activated.

Cursor-transition primitives use an in-memory copy of the one physical-source
plan, preserving its exact native directory count and original pin. They do not
modify published descriptors, HASHes or indexes. They continue requiring unsafe
bounded cursor opens/pages to fail, retain one aggregate per failed range and
verify no cursor leaks. Ordinary HASH fallback retains exact values and global
LIMIT/OFFSET.

A separate native segmented case retains the actual overlay plan. After initial
index validation, a readiness transition on the original segment must preserve
newest versions and a deletion through exact fallback. All 200 globally ordered,
offset result values are checked independently, with no aggregate opened against
the injected building source and no cursor leak or metadata change. This is not
permission to open a catalog whose initial published-index validation fails.

The all-primary topology case installs its non-coordinator node hook before
starting the pin. It independently requires the coordinator to remain ready;
proxy execution skips this case and does not prove Cluster API coverage.

Verified on owned Redis Software with effective active 4x QPF and noeviction,
after the preceding mutating suite completed:

- Focused race selection: 19 passed, one explicit Cluster API topology skip,
  zero failures; root 6.652 s.
- Formatting and diff whitespace checks pass.

No production changes are needed for this fixture restoration. Full ordinary,
restricted-reader integration, current-protocol Cluster failover/AOF/RDB recovery,
hosted/platform, process/input resume and sustained/comparative performance gates
remain open. These cases are not a complete ordinary-suite pass.
