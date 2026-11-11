# Validation

Date: 2026-11-10. Source revision: `61043e1a`. Status: documentation only.
Requestor: repository user. No issue created; the pull request is tracked in GitHub.

Expanded `docs/integrations/sling.md`; no implementation changed.

- All 15 shell fences passed `bash -n`.
- Both replication YAML examples parsed with Ruby YAML and contained source, target, defaults and streams. Default Python lacked PyYAML; Ruby was used instead.
- JSON examples parsed with Python `json.loads`.
- All local Markdown file-link targets exist; the PostgreSQL comparison heading was checked against `go/pgdiff/README.md`.
- Official Sling pages were read for installation/version selection, adapter inventory, environment connections, ADBC manager/library setup, MySQL, Snowflake, local files, replication structure and direct insertion. Links are in the guide.
- Commands/options were compared with `scripts/test_sling_postgres.py`, `scripts/sling/adbc.yaml`, `go/Makefile` and the HASH CLI source.
- Review against `REVIEW.md`: source HASH ownership, private configuration, destructive modes, autocommit, failure behavior and historical evidence are explicit. No human approval is inferred.
- `git diff --check` passed.

No live transfers, Software startup, benchmark or Go suite was run for this documentation-only change. Existing PostgreSQL round trips and the 602/651 SQL comparison are cited historical evidence, not fresh validation. MySQL, CSV, Snowflake, HASH-to-Sling chaining, the small seed walkthrough and YAML invocations were not executed.

See [intent](intent.md), [spec](spec.md) and [plan](plan.md).

README follow-up, 2026-11-10: expanded the existing Sling entry in the tools list to describe SQL-to-HASH migration and exports through its adapter ecosystem, retaining the guide link and the distinction between tested PostgreSQL transfers and additional examples. Confirmed one Sling row and an existing guide target. The integration registry suite passed all 10 tests; `git diff --check` passed. No live transfers were rerun.

Connector inventory follow-up, 2026-11-10: replaced the illustrative adapter table with all database, file-system and API connector names displayed at `https://slingdata.io/connectors/`. Preserved the site’s names, distinguished its headline counts from displayed entries, and retained direction-specific and Redis interoperability limits. Local link targets and `git diff --check` passed; no live transfers were rerun.
