# SQL compatibility and typed Arrow projection

2026-10-22. Baseline: `d72b730`. Requestor: alberttwong. Author: Codex.
Status: implemented and validated; PR preparation. [Spec](spec.md), [plan](plan.md).

Implement #241 (pagination expressions), #188 (DATE function casts), #244
(VALUES queries), #242 (aggregates without FROM), and investigate #446
(prebound typed managed-scan decoding/appending) in one reviewable PR.
Preserve PostgreSQL result types, NULLs, errors, prepared reuse and Arrow lifetime.
The Arrow prototype must pass independent full-result performance gates before
promotion; retain an unsuccessful experiment rather than claiming a speedup.
No transaction, server export, storage layout or query eligibility changes.
