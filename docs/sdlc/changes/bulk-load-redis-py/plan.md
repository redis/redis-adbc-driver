# Bulk-loading implementation and validation

2026-10-22. See [intent](intent.md) and [spec](spec.md).

1. Build the pinned current driver and provision a dedicated local container.
2. Add the standalone runner; validate all four cases and preserve diagnostics.
3. Run six measured rounds plus warmups; recompute summaries and all hashes.
4. Retain the evidence/report, update the Google Slides deck and inspect its
   rendered output. Remove the task-owned container after collecting evidence.

Slide construction: duplicate the existing native matched-product comparison
slide (`redis_matched_transfer_comparison_20261021`) and reuse its table, title,
subtitle, callouts, notes and footer. No media slots exist in this exemplar.
The new role is the write-side comparison before the read-side evidence. Map
all inherited text to new measured content; preserve existing slide content
apart from numbering and a narrative transition.

Completed: all 28 canonical executions and four final smoke executions returned
exact source values/multiplicities and complete indexes. The one-writer median
is 7.575 s for ADBC versus 9.712 s for redis-py (22.0% less elapsed time).
Four-writer medians are 6.402/6.546 s with overlapping observed ranges.
See the [report](../../../../benchmarks/bulk-load/2026-10-22/README.md) and
[validation](validation.json). All summary statistics, hashes and local links
were checked; the four focused helper tests passed. Two earlier harness-only
smoke failures are retained separately. No production code changed.

Added native slide 8 to the existing deck, renumbered the following slides and
added full speaker notes, including the higher ADBC peak worker RSS. Live
structural readback confirmed 15 slides and the requested position. Exported
the complete deck as PDF and visually inspected the new slide and all 15 slides
in a contact sheet; no clipping, placeholders or layout repair was needed.
The current connector has no get_presentation action, so inspection used the
live visible DOM plus browser PDF export rather than the unavailable native
JSON/template parser. Original table/text styles were preserved.

The task-owned container codex-bulk-load-20261022 was removed after saving
evidence. Codex completed the three REVIEW.md passes; this is agent review,
not independent human approval. Benchmark files remain local for review.
