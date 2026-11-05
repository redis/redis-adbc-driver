# Specification: Substrait design milestone

Date: 2026-10-30. Source: `95d6583d2b07bdfdadc6ddf3b6368bf631139c24`.
Status: proposed contract, documentation delivered. Author: Codex.
[Intent](intent.md), [plan](plan.md), [evidence](validation.md),
[issue #213](https://github.com/redis/redis-adbc-driver/issues/213).

## Contract and implementation boundaries

The canonical technical specification is [Substrait feasibility and proposed
subset](../../../substrait.md). It contains the source assessment, protocol pin,
relation grammar, exact type/function allowlist rules, table/schema discovery,
parameter exclusions, budgets, statuses and lifecycle/capability gates.

Current runtime behavior stays unsupported. No Go dependency, C ABI or SQL
behavior changes in this milestone. The first implementation must validate all
input before lowering to a driver AST and reuse existing execution/cleanup.

## Acceptance and risks

The design must address all proposed-scope bullets in #213 and give executable
acceptance cases for its later runtime phase. Documentation links must resolve,
and present/proposed behavior must remain distinguishable. The plan records
unperformed runtime checks rather than claiming they passed.

Primary risks are ordinal remapping, SQL/Substrait type/null semantic gaps,
unbounded translation/scan work, C copying before admission checks, stale table
metadata, and leaked Arrow/cursor resources. Each has an implementation or test
gate in the canonical specification and [plan](plan.md).
