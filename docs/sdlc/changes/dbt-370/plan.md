# Plan and evidence: dbt comparison correction

Date: 2026-10-21. Source: `95d6365bb76212a49888bfec423cf8c8b15e61a7`.
Requestor: repository owner, via chat. Author: Codex. Status: proposed for review.
Issue: [#171](https://github.com/redis/redis-adbc-driver/issues/171).

## Work

1. Inspect companion v1/v2 guides, v2 validation record and engine pin.
2. Verify linked Linux CI run status through the GitHub API.
3. Update row 7, comparison caveat, implications and testing status in
   `docs/data-engineering-adbc-support.md`.
4. Check Markdown table structure, diff and links; submit a PR referencing
   #370 without closing its remaining Flight SQL work.

## Verification and review

- GitHub API: native v2 run `39623533131` is completed/success.
- Inspected companion `v1/README.md`, `v2/README.md`, `v2/VALIDATION.md`,
  and `v2/upstream.json`; v2 source pin is
  `90178580bd6f3353d758fff770fdc2c58c985ad0`.
- `git diff --check`: passed. Markdown table: 22 data rows, nine columns each.
- Documentation-only: integration suites were not rerun. Existing evidence
  supports the stated scenarios only, not stock-engine or Flight SQL support.
- Self-review against REVIEW.md: no actionable findings; no runtime changes.
  Human review decision remains pending on the PR.

See [intent](intent.md) and [spec](spec.md).
