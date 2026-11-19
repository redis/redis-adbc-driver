# Release v0.0.29 slide benchmark refresh

Date: November 18, 2026. Requestor: user in the benchmark-refresh chat. Status: delivered.

Rerun the performance deck's current bulk/read/creation and point/query matrices
with the latest published driver, then update the existing native Slides tables,
methodology notes and date/version labels. Retain complete independently validated
results, including slower cases, warmups and one/four-client observations.

Historical ClickBench results retain their original distribution and settings.
No production driver changes, loading-performance study or full-scale ClickBench
rerun is included. Use released checksummed Linux arm64 library and CLI artifacts,
an owned Redis Software fixture, verified effective 4× QPF and trusted TLS/RESP3.

See [specification](spec.md) and [plan](plan.md).
