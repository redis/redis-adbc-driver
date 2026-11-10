# Plan and validation

2026-11-10 · Source: 85509485

1. Read the repository inventory and supplied presentation's category inventories.
2. Reorder the existing rows into the deck's two areas and ten subcategories;
   add a category index and clarify original numbering.
3. Verify row preservation, category membership/counts, unchanged evidence links,
   category anchors, local link targets, and the existing inventory coverage checks.
4. Review the documentation diff against the grouping contract.

Validation:

- `PYTHONPATH=scripts python3 -m unittest scripts.test_distribution.InstallationCoverageTests scripts.test_acceptance_runner.AcceptanceRunnerTests.test_audit_covers_every_comparison_row`:
  two passed. The initial invocation without `PYTHONPATH=scripts` could not import
  `verify_release_wheels`; the corrected invocation above passed.
- One-off comparison against `HEAD`: all 50 tool rows preserved verbatim and
  present exactly once; exact category membership and 27/23 totals verified;
  ten table headers and ten category anchors verified; all original inline
  links and post-inventory evidence unchanged; 72 local inline targets and
  change-record links resolved.
- PR #372 follow-up: CI's strict MkDocs build rejected eight category links
  because MkDocs collapses punctuation-related hyphens differently from GitHub.
  Reproduced all eight warnings locally with MkDocs 1.6.1. The original custom
  slug check verified only GitHub-style anchors and was insufficient for the site.
- Added explicit category anchors and normalized index targets. Using the existing
  `/private/tmp/redis-adbc-docs-venv/bin/python` (MkDocs 1.6.1), ran
  `scripts/prepare_docs.py`, `-m mkdocs build --strict`, and
  `scripts/check_docs.py`: passed, 41 pages verified. A separate rendered-HTML
  check verified all ten category index targets; inventory coverage checks still
  passed (two tests), and all 50 original tool rows remain unchanged.
- `git diff --check`: passed.
- Reference deck read through the Google Drive connector; only its grouping
  was reused. The GitHub web fetch returned a cache miss; repository content
  was read from this checkout. No external evidence re-audit or integration
  rerun was performed for this documentation-only change.

Review: compared the diff with the grouping contract, checked support and
ownership claims remain verbatim, and verified existing installation/acceptance
inventory consumers still resolve all 50 numbered tools. No actionable findings.
This is an agent review, not maintainer approval.

See [intent](intent.md) and [spec](spec.md).
