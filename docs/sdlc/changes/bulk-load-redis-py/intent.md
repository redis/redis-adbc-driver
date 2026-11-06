# Bulk-loading comparison intent

2026-10-22. Requested by Albert Wong; author: Codex. Source commit:
`d72b730bf24074942609e9f0f839c1f46e10d365`. Status: benchmark and slide update complete.
See [spec](spec.md) and [plan](plan.md).

Measure bulk ingestion against the official redis-py client, then add a slide
to the existing Redis ecosystem deck. Previous client comparisons measured
reads; existing ingest figures lack a matched native reference. Report the
result regardless of which client wins. No production driver changes.
