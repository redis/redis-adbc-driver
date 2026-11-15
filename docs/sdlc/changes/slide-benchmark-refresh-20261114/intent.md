# Intent: refresh the performance-slide benchmarks

[PR #442](https://github.com/redis/redis-adbc-driver/pull/442).

Requested by the user on November 14, 2026: rerun the benchmarks on latest code so the performance slides can use current numbers, then create a PR.

Measure managed mode with default settings, live unmanaged mode, and redis-py+hiredis across point access/mutations, bulk SQL and Arrow writes, create/create_append, wide reads, filters, aggregates and data movement. Use clean main `34f4c82972c7a059f045f1fb65bdfc2ce4adfce9` and a native Linux client beside an owned Redis Software fixture to avoid conflating the previously observed macOS published-port delay with SQL execution.

No production behavior change, default option change or ClickBench rerun. Historical cross-product results retain their original server, hardware and metric labels. This is a current reference, not a matched before/after speedup experiment.

See [spec](spec.md), [plan](plan.md), and the [benchmark report](../../../../benchmarks/table-modes/slides-refresh-2026-11-14/report.md).
