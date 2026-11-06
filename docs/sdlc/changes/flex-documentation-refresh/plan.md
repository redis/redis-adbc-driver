# Plan: publish the Software Flex documentation refresh

Date: 2026-11-05. Source: main `43f4f061`. Author: Codex.
Inputs: [intent](intent.md), [spec](spec.md). Status: checks complete; ready for documentation review.

## Files and work

1. Correct the [README](../../../../README.md) support row and feature summary.
2. Refresh the [guide](../../../redis-flex-stopgap.md) from merged PR #331 evidence.
3. Distinguish the [initial report](../../../../benchmarks/redis-flex/2026-11-05/README.md)
   from the [follow-up](../../../../benchmarks/redis-flex/2026-11-05/followups/README.md),
   and explain ratio calculation and RAM control variability.
4. Mark [initial validation](../322-flex-stopgap/validation.md) as historical and
   update [follow-up records](../324-330-flex-followups/validation.md) to merged.
5. Verify numeric provenance, links/anchors, privacy and whitespace; open a
   separate PR against main with remaining failures and scope explicit.

## Verification and review

- All 360 warm raw samples match all 72 summary groups (five trials each).
  Latest mean latencies recompute to 287.93/887.13/1,981.52 ms and the headline
  multipliers to 3.1x RAM stopgap / 6.9x Flex stopgap versus RAM default.
- Published native counts, three promoted replicas, 25,000 validated failover rows
  and active QPF 4 are checked against committed JSON evidence.
- All 159 local links/anchors across 12 changed Markdown files resolve; code
  fences are balanced and `git diff --check` passes.
- Correctness/evidence review preserves dated failures and consistency limits;
  ownership/privacy review finds no new credentials or operational identifiers;
  scope review confirms only Markdown changes and unchanged measurement data.

No new driver, Redis, Cloud or benchmark execution is claimed. This is agent
self-review, not maintainer approval. Recorded measurements remain historical.
