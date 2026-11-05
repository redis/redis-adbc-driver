# Support refresh validation

Date: 2026-11-02. Base: `aa33d73`. Documentation only.

- Fetched current `redis/redis-adbc-driver` main and public PR metadata; latest merged PR was #282. The initial worktree was on unrelated older history, so the documentation branch starts from current main.
- Checked #282 against `integration-acceptance/validation.md` and `fixed-results.json`: SQLAlchemy 677 passed/683 skipped; patched R/DBI 9,385 passed/14 skipped; ADBC 477 passed/8 skipped. Fivetran's one-pass earlier SDK tester run remains separately attributed. Zero failures/errors in these recorded results; no suites rerun here.
- Checked #281 against `integration-distribution/validation.md` and the installation guide: 29 focused checks and local test-wheel evidence; merged automation is not a completed subsequent release.
- Checked all 50 table rows, ten columns per row, 41 qualifying badges, 37 ADBC badges and 25 Flight SQL badges. All 129 relative file links resolve. `git diff --check` passed.
- Corrected Fivetran/Superset badges because their implemented local paths already meet the inventory's SQL/Arrow criteria. Retained hosted-deployment/browser limits, read-only source direction and separate Flight SQL evidence.
- Edited the existing native [ecosystem deck](https://docs.google.com/presentation/d/1CdXUMzl7VJNHgVke0hR0rl8n3TmqzcV19gNrnT8l-d8/edit) in place. Added two slides by duplicating the existing three-row strategy exemplar, preserving native text, geometry and typography. Synced counts, appendices and affected speaker notes.
- Full native Slides read tools were unavailable in the connected tool set. Used authenticated PPTX structural/style exports, native UI IDs and ordered PDF renders as the fallback. Inspected all 25 source slides and all 27 delivered slides locally; no clipping/overlap found in the changed content. Output order and notes were verified through UI/PPTX readback. A final notes-only Airbyte correction does not change the rendered slide.
- Historical benchmarks retain their actual dates, versions, distributions and limitations. No benchmark or integration runtime was executed for this refresh.

Review: checked correctness/evidence scope, data ownership and intended documentation behavior against REVIEW.md. No runtime/security/release permission changes. Human PR review remains pending; this record is not approval to merge.
