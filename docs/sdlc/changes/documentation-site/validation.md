# Validation: public documentation website

Date: 2026-11-10. Base: `73eb9642`. Status: local checks passed.

- Staged 41 explicitly selected Markdown files with `scripts/prepare_docs.py`.
- `python -m mkdocs build --strict`: passed, including fragment-link validation.
- `python scripts/check_docs.py`: passed for 41 HTML pages, local links/assets,
  canonical URLs, sitemap, search coverage and publication scope.
- Rebuilt and checked with `DOCS_SITE_URL=https://docs.example.org/driver/`:
  passed; restored the default URL and rechecked afterward.
- Inserted a temporary synthetic `.jsonl` artifact into the output: the site
  check rejected it as expected. Removed it before publication.
- `git diff --check`: passed.
- Redis/driver tests: not run; no driver behavior changed.

Agent review found no actionable correctness or publication-scope findings.
PR builds cannot deploy. Main-branch deployment receives only Pages/OIDC write
permissions; source builds have read-only repository access. This is agent
review, not human approval. Generated files stay under ignored `build/`.

GitHub Pages was not enabled when inspected. Enabling it through `gh api` returned
403 because the configured token cannot access that operation. The requestor
will select GitHub Actions in Pages settings. Live deployment and Google
indexing are not established by these local checks. See [plan](plan.md).
