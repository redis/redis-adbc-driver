# PostgreSQL 18 overlaps and input errors: plan

Date: 2026-11-10. Author: Codex. Requestor: Albert Wong.
Source: `61043e1a`. Status: implemented; local validation complete; pending PR review.
Issue: [#110](https://github.com/redis/redis-adbc-driver/issues/110).

See [intent](intent.md), [spec](spec.md), [plan](plan.md).

1. Capture both original failures before implementation.
2. Add operator parsing, temporal overload selection and exact evaluation; implement diagnostic record typing, scalar evaluation and FROM expansion.
3. Retain independent adversarial cases outside the default corpus: endpoint/NULL matrix, temporal families, intervals, DST, precedence, signatures, diagnostics and record consumers.
4. Run Go units, harness tests, focused live suites and the unchanged full differential corpus on Redis Software with active verified 4x QPF.
5. Compare every prior pass, retain sanitized results/source hashes, update current profile/inventory, review using REVIEW.md and submit a PR. Raw operational reports remain private under ignored build paths.

All five implementation/verification steps completed. See [validation](validation.md) and [sanitized results](results.json); CI and human review are recorded on the PR.
