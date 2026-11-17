# Intent

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: b63a010d. Status: implementation and validation, not merge approval.

Continue [#446](https://github.com/redis/redis-adbc-driver/issues/446) after
[#456](https://github.com/redis/redis-adbc-driver/pull/456). Remove the reproduced
integer-to-pointer checkptr blocker without disabling checks or changing forced
GC, so the C/Arrow ownership gates can execute.

See [spec](spec.md), [plan](plan.md) and [validation](validation.md).
