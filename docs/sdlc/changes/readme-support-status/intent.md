# README support status intent

Date: 2026-11-04. Base: `b1d1bf005780516a17ceaf46ed49840506c77b20`.
Requestor: Albert Wong. Author: Codex. Status: implemented; awaiting PR review.

The README labels completed Arrow/ADBC contracts Partial because of intentional
SQL normalization and scoped API limits. Readers cannot distinguish accepted
behavior from unfinished work when the linked issue is closed.

Mark implemented, explicitly scoped contracts Supported; retain every limit.
File labeled, nonduplicate follow-ups for missing capabilities and link them
separately. Keep unsupported layouts and experimental device/GPU work honest.
No runtime, transaction, consistency or release behavior changes.

See [specification](spec.md), [verification plan](plan.md), and the
[README](../../../../README.md#apache-arrow-feature-matrix).
