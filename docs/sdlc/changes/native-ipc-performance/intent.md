# Intent: matched native IPC performance

Date: 2026-10-22. Requestor: Albert Wong. Author: Codex.
Base: `e2e7725c3b7be15ff9e657b9edc2a1488e676b05`. Status: measured; review pending.

Measure the native IPC file-path interface from #458, with the merged #462
UNION ALL reader, against client PyArrow serialization/deserialization on the
same driver binary. The user requested an actual targeted rerun before native
IPC performance claims. Retain historical transfer results as dated baselines.

Cover narrow/wide scans, unordered UNION ALL and a driver-side blocking
aggregate; compare narrow/wide imports in both stream and file formats.
Validate every value outside operation timing, and record complete-operation
elapsed time, process CPU, RSS, rows/bytes per second and file size. Use an owned
Redis Software Enterprise fixture with effective QPF 4x verified. No CDC, paid
deployment, full ClickBench rerun, network IPC or durable-storage claim.

See [spec](spec.md), [plan](plan.md), and [report](../../../../benchmarks/native-ipc/2026-10-22/README.md).
