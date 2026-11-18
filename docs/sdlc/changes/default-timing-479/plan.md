# Plan

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `5b0840ce`. Status: implemented; local verification complete; PR review pending.
Issue: [#479](https://github.com/redis/redis-adbc-driver/issues/479).

1. Probe PostgreSQL18 and retain failing definition/use-phase regressions before changing behavior.
2. Split definition validation from value evaluation and preserve ADD COLUMN materialization.
3. Verify CREATE/ALTER/TYPE, literal/typmod errors, explicit and NULL values, lazy/default/empty inputs, prepared/bulk binding, volatile/sequence behavior and atomic failures on verified Software4xQPF.
4. Review scope and evidence, clean own resources and open a PR closing #479 if all closure gates pass.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
