# Intent

Albert Wong requested fixing the same-table managed SQL serialization conflicts
on November 12, 2026. Coordinate DML writers before execution, preserve snapshots
and never replay SQL. See [spec](spec.md), [plan](plan.md).
