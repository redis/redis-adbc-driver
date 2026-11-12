# README flow plan and verification

Date: 2026-11-12. Base: `53b76248`. Requestor: Albert Wong. Author: Codex.
Status: implemented locally; not released.

See [intent](intent.md) and [specification](spec.md).

1. Read repository guidance, the README, setup/snapshot/integration guides and
   the current example connection helper, Makefile and Software startup script.
2. Rewrite the README, relocate detailed matrices and sample queries, and update
   the integration index introduction to match the new navigation. Include the
   relocated guides in the explicit MkDocs publication/navigation list.
3. Check links, feature-row preservation, whitespace and the final diff.

Verification completed:

- All 149 local Markdown targets/fragments across the README, relocated guides
  and integration index passed a local check.
- Both feature matrices retain all 59 original rows, including headers, in order.
  Updated the stale RBAC restriction to match the current SQL RBAC guide.
- Quick-start environment sourcing matches `go/software-setup.sh`; the Python
  helper reads `REDIS_URI`. Removed obsolete physical-key inspection examples
  and in-place adoption claims in favor of the current snapshot contract.
- Strict MkDocs build and `scripts/check_docs.py` passed for all 44 published
  HTML pages, including links/assets, canonical URLs, sitemap and search.
- `git diff --check` passed. No runtime tests or benchmarks were run for this
  documentation-only change; linked historical evidence remains historical.

Self-review under REVIEW.md checked correctness, ownership/privacy and alignment
with the requested flow. No runtime or storage behavior changed. Human review
and release validation are outside this local edit.
