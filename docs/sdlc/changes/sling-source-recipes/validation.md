# Validation: Sling source recipes

Date: 2026-11-17. Source: `5fe2bb76cacca9a385479f56b739aeb8e63b3838`.
Author: Codex. Status: static checks passed; live recipes unvalidated.
[Intent](intent.md), [specification](spec.md), [plan](plan.md).

The worktree matched fetched `origin/main` before editing. Reviewed the official
Sling PostgreSQL, MySQL, MongoDB, Snowflake, Redshift and BigQuery connector docs,
replication/target options, and Sling v1.6.6 export implementations. The guide
links the connector references and versioned source for warehouse export options.

Static verification passed:

- All 26 fenced blocks are balanced; all 22 shell blocks pass `bash -n`.
- Three literal connection JSON exports, seven target-option JSON values and
  the standalone JSON example parse; both embedded Python blocks pass AST parsing.
- All 28 local links/anchors in the guide and change records resolve.
- All six newly introduced external URLs return HTTP 200. Existing MySQL and
  Snowflake connector links were also read as setup evidence.
- `git diff --check` passes. No repository links use the removed section anchors.
- Reviewed the diff against the specification and repository review passes;
  no actionable findings remain from static review. No credentials were used.

The two unchanged replication YAML examples were reviewed manually; PyYAML is
unavailable in the system Python. An initial link check ran before this validation
record existed; the final check passes with all four change records present.

No live source connections, Redis tests, driver builds or cloud provisioning
were performed for this documentation-only change. The historical PostgreSQL
round-trip evidence remains linked and labeled; the other five recipes have not
been validated end to end. Static review is not a human merge approval.
