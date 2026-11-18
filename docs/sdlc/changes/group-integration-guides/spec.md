# Specification: integration index categories

Date: 2026-11-18. Author: Codex; requestor: repository user. Base: `8eb98ae0`.
Status: implemented and verified; awaiting human review. No separate tracking issue.

See [intent](intent.md) and [plan](plan.md).

Replace the flat table with the ecosystem comparison's Data engineering and
Data science & BI areas and its nine categories that have dedicated guides.
Keep each tool in its comparison category and retain comparison entry order.
DuckDB has a dedicated guide but no comparison row; include it in SQL & Arrow
access and state that addition. Omit categories without dedicated guides rather
than adding new integrations. Add a linked category table.

Acceptance: all 39 original rows appear exactly once with identical descriptions
and links, category anchors resolve, and the documentation build and publication
checks pass. This is an index-only edit; guide contents and site navigation stay
as they are. No new compatibility or validation claims are introduced.
