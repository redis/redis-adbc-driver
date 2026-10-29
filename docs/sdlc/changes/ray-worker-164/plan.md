# Ray worker reader plan

Date: 2026-10-29. Author: Codex. Baseline: `34ae626`.
Status: implementation and local validation complete; PR/human review pending.
See [intent](intent.md), [spec](spec.md) and [validation](validation.md).

1. Add optional pinned Ray dependency and RedisDatasource/read_query helper.
2. Plan once, serialize descriptors/schema/configuration and stream worker batches.
3. Exercise lifecycle failures and real Ray worker/TLS/retry behavior.
4. Add an isolated Python 3.12 Ray CI step on amd64 Software single-shard jobs.
5. Document installation, scope and consistency; update the comparison and #164.
6. Submit the implementation PR; retain upstream acceptance as separate work.

The empty-result test exposed Ray dropping empty output blocks: supply schema
through ReadTask metadata and test Dataset.schema before/after execution.
TLS failure testing exposed unserializable ADBC errors: normalize native read
errors to RuntimeError while preserving useful error messages.

No other runtime suites were changed. Multi-node execution, performance,
installed release wheels and further Ray versions remain unvalidated.
