# Issue drafts (2026-11-24, not filed)

Drafts for review. Nothing here has been posted to GitHub. Each file starts with a header (`Target`, `Title`, `Labels`, sometimes `Status`) followed by `---` and the body to post.

Checked against `origin/main` 682fae1f. Re-check upstream states on the day you file: several depend on releases that hadn't shipped as of 2026-11-24.

## New issues in redis/redis-adbc-driver

| File | Target | Title | Labels | Status |
|---|---|---|---|---|
| [analysis-mlflow-stale-xfail.md](project/analysis-mlflow-stale-xfail.md) | redis/redis-adbc-driver — new issue | Analysis profile: remove stale MLflow 3.17.2 strict xfail and lift the PyArrow <26 cap (fixed in MLflow 3.18.0) | bug, partner integration | small enough to fix directly instead of filing. Not run: the test outcome under MLflow 3.18 is inferred from the code. |
| [arrow-go-next-release-cleanup.md](project/arrow-go-next-release-cleanup.md) | redis/redis-adbc-driver — new issue | Remove Arrow Go workarounds that upstream has fixed (now, and after the next arrow-go release) | enhancement | — |
| [docs-stale-references.md](project/docs-stale-references.md) | redis/redis-adbc-driver — new issue | Docs: refresh stale issue/PR status references (Superset #471, closed trackers, submitted upstream reports) | documentation | — |
| [driverbase-ffi-followups.md](project/driverbase-ffi-followups.md) | redis/redis-adbc-driver — new issue | driverbase-go FFI follow-ups: adopt the agreed GC-removal plan (#243) and keep the hand-patched bridge from regressing (#245/#246) | enhancement | — |
| [go-redis-v9-24-bump.md](project/go-redis-v9-24-bump.md) | redis/redis-adbc-driver — new issue | Bump go-redis to v9.24.0 for the Tx.Close WATCH/MULTI cleanup fix (go-redis#4315) and re-qualify | bug | — |
| [integration-tests-large-fixtures.md](project/integration-tests-large-fixtures.md) | redis/redis-adbc-driver — new issue | Integration tests fail on large or shared fixtures (fixed 10 s cleanup SCAN and index-readiness deadlines, live-table capacity) | bug, needs-triage | — |
| [join-reader-row-allocation.md](project/join-reader-row-allocation.md) | redis/redis-adbc-driver — new issue | Reduce per-row allocation in the streaming star/snowflake join readers (GC is 40–50% of CPU on TPC-H Q9) | enhancement | — |
| [join-sort-spill.md](project/join-sort-spill.md) | redis/redis-adbc-driver — new issue | Bound client memory for eager joins and full sorts: no spill path today, and the snowflake cap falls back to the eager join | enhancement | — |
| [olap-coordinator-oom.md](project/olap-coordinator-oom.md) | redis/redis-adbc-driver — new issue | OLAP harness: a query worker's OOM can kill the coordinator because both share one client memory cgroup | enhancement | — |
| [pg18-sql-cursors-check-option.md](project/pg18-sql-cursors-check-option.md) | redis/redis-adbc-driver — new issue | PostgreSQL 18: SQL cursors (DECLARE/FETCH/CLOSE) and CREATE VIEW … WITH CHECK OPTION have no tracking issue | enhancement, needs-triage | — |
| [rekey-concurrent-writers-arm64-flake.md](project/rekey-concurrent-writers-arm64-flake.md) | redis/redis-adbc-driver — new issue | CI flake: TestSQLRekeyConcurrentWriters intermittently fails on the hosted linux-arm64 Software cluster job | bug, needs-triage | — |
| [semijoin-strategy-and-q21-outlier.md](project/semijoin-strategy-and-q21-outlier.md) | redis/redis-adbc-driver — new issue | Semi-joins: choose page probes or the whole decorrelated build by cost, and explain a 70.9 s Q21 outlier (low priority) | enhancement | — |
| [sf1-near-deadline-queries.md](project/sf1-near-deadline-queries.md) | redis/redis-adbc-driver — new issue | SF1 queries passing with little margin: TPC-H Q10/Q12/Q13/Q20 (29–38 s) and SSB Q2.1/2.2/3.1/4.1/4.2 (21–32 s) against a 60 s deadline | enhancement | — |
| [sling-v1-6-8-revalidation.md](project/sling-v1-6-8-revalidation.md) | redis/redis-adbc-driver — new issue | Sling: re-validate on v1.6.8 (sling-cli#836 fixes) and retire template/precreation workarounds | partner integration, documentation | actionable only after Sling v1.6.8 is released (latest is v1.6.7 as of 2026-11-24). It could be filed now as a reminder. |
| [tpcds-sf1-full-suite-current-main.md](project/tpcds-sf1-full-suite-current-main.md) | redis/redis-adbc-driver — new issue | TPC-DS SF1: run Q1–Q99 on one current-main build (Q10 was interrupted, Q11–Q99 never ran on Redis) | enhancement, needs-triage | — |
| [tpch-sf1-ram-reverification.md](project/tpch-sf1-ram-reverification.md) | redis/redis-adbc-driver — new issue | TPC-H SF1 on Software RAM: re-verify Q1–Q22 on current main after the subset-based closures of #578–#587 and #599 | enhancement, needs-triage | — |

## Upstream reports

| File | Target | Title | Labels | Status |
|---|---|---|---|---|
| [adbi-difftime-time-binding.md](upstream/adbi-difftime-time-binding.md) | r-dbi/adbi — new issue (plain .github/ISSUE_TEMPLATE.md: brief description + reprex) | dbBind() sends difftime parameters as Arrow duration even when the prepared parameter schema asks for time32/time64 | — | R reproduction NOT executed (adbi and nanoarrow are not installed here); the expected output comes from reading the sour |
| [arrow-go-cdata-union-import-panics.md](upstream/arrow-go-cdata-union-import-panics.md) | apache/arrow-go — new issue (Bug Report form; Component: Other) | arrow/cdata: ImportCArray panics on malformed sparse union input (short children, duplicate type codes) | per upstream template ("Type: bug") | reproduced locally on v18.9.0 (Go 1.27.2, darwin/arm64); upstream main a7fe8f8ba4 (2026-11-24) has the same code paths |
| [nanoarrow-r-posixct-int64-double.md](upstream/nanoarrow-r-posixct-int64-double.md) | apache/arrow-nanoarrow — new issue (no issue template; use the "[R]" title prefix seen o | [R] Timestamp → POSIXct casts int64 ticks to double before scaling; every current nanosecond timestamp triggers a lossy-conversion warning | — | R reproduction NOT executed (nanoarrow is not installed here); the expected output comes from reading the 0.9.0 source. |
| [redisearch-flex-doctable-ram-quota.md](upstream/redisearch-flex-doctable-ram-quota.md) | RediSearch/RediSearch — new issue (bug_report.md template: "[BUG]" prefix, label `bug`;  | [BUG] Search on Flash: DocTable metadata fills the Flex RAM quota during a large HASH index backfill (OOM at ~31M documents) | — | facts match #577 and its record. Before posting: confirm the host/OS (not recorded), and use an account with RediSearch  |
| [redisearch-flex-tag-compaction-abort.md](upstream/redisearch-flex-tag-compaction-abort.md) | RediSearch/RediSearch — new issue (bug_report.md template: "[BUG]" prefix, label `bug`). | [BUG] Search on Flash: TAG compaction fails with "operand's last doc id is not greater than previous operands'" and the shard aborts (SIGABRT) while indexing ~100M HASHes | — | facts match #574 and benchmarks/clickbench/managed-ingestion-2026-11-20/issue-566.md:79-130. Before posting: record the  |
| [sqlalchemy-suite-requirement-gates.md](upstream/sqlalchemy-suite-requirement-gates.md) | sqlalchemy/sqlalchemy — new GitHub Discussion, category "Usage Questions" (bug_report.ya | Third-party dialect suite: InsertBehaviorTest and reflection fixtures aren't gated by autoincrement_insert / index_reflection | — | optional, low priority. The gaps are confirmed in the 2.1.6 source and on main 9c88919b; not a runnable MCVE. |

## Comments on existing issues

| File | Target | Title | Labels | Status |
|---|---|---|---|---|
| [460-arrow-go-fix-merged.md](comments/460-arrow-go-fix-merged.md) | redis/redis-adbc-driver — comment on #460 | — | — | — |
| [577-upstream-draft.md](comments/577-upstream-draft.md) | redis/redis-adbc-driver — comment on #577 | — | — | post this together with the RediSearch report, replacing the placeholder with its URL. |
| [636-flex-worker-cap-and-overlays.md](comments/636-flex-worker-cap-and-overlays.md) | redis/redis-adbc-driver — comment on #636 | — | — | — |

## Filing

The body is everything after the first `---` line. For a project issue:

```bash
f=issue-drafts/project/go-redis-v9-24-bump.md; gh issue create -R redis/redis-adbc-driver --title "$(sed -n 's/^Title: //p' "$f")" --label bug --body "$(sed '1,/^---$/d' "$f")"
```

For a comment on an existing issue:

```bash
f=issue-drafts/comments/636-flex-worker-cap-and-overlays.md; gh issue comment 636 -R redis/redis-adbc-driver --body "$(sed '1,/^---$/d' "$f")"
```

Upstream reports use each project's own template or form (named in the `Target` line). Paste the body into the web form, rather than using `gh`, so the form's required fields get filled.

Order that matters:
1. File `project/arrow-go-next-release-cleanup.md` before posting `comments/460-arrow-go-fix-merged.md`, and put its link where that comment has a placeholder.
2. Post the two RediSearch reports before `comments/577-upstream-draft.md`, and put their URLs into it.

## Filed 2026-11-24

| Draft | Posted |
|---|---|
| arrow-go-next-release-cleanup.md | https://github.com/redis/redis-adbc-driver/issues/647 |
| tpch-sf1-ram-reverification.md | https://github.com/redis/redis-adbc-driver/issues/648 |
| tpcds-sf1-full-suite-current-main.md | https://github.com/redis/redis-adbc-driver/issues/649 |
| go-redis-v9-24-bump.md | https://github.com/redis/redis-adbc-driver/issues/650 |
| analysis-mlflow-stale-xfail.md | https://github.com/redis/redis-adbc-driver/issues/651 |
| join-reader-row-allocation.md | https://github.com/redis/redis-adbc-driver/issues/652 |
| integration-tests-large-fixtures.md | https://github.com/redis/redis-adbc-driver/issues/653 |
| join-sort-spill.md | https://github.com/redis/redis-adbc-driver/issues/654 |
| driverbase-ffi-followups.md | https://github.com/redis/redis-adbc-driver/issues/655 |
| sling-v1-6-8-revalidation.md | https://github.com/redis/redis-adbc-driver/issues/656 |
| docs-stale-references.md | https://github.com/redis/redis-adbc-driver/issues/657 |
| sf1-near-deadline-queries.md | https://github.com/redis/redis-adbc-driver/issues/658 |
| pg18-sql-cursors-check-option.md | https://github.com/redis/redis-adbc-driver/issues/659 |
| olap-coordinator-oom.md | https://github.com/redis/redis-adbc-driver/issues/660 |
| rekey-concurrent-writers-arm64-flake.md | https://github.com/redis/redis-adbc-driver/issues/661 |
| semijoin-strategy-and-q21-outlier.md | https://github.com/redis/redis-adbc-driver/issues/662 |
| comment-460 | https://github.com/redis/redis-adbc-driver/issues/460#issuecomment-7153132297 |
| comment-636 | https://github.com/redis/redis-adbc-driver/issues/636#issuecomment-7153132728 |
| comment-577 | https://github.com/redis/redis-adbc-driver/issues/577#issuecomment-7153518069 |

## Upstream reports posted 2026-11-24

`gh` couldn't post these: the `GH_TOKEN` it uses is refused for issues and discussions in other organizations' repos. They were posted through the in-app browser instead, each body checked against its draft by SHA-256 before submitting.

| Draft | Posted |
|---|---|
| redisearch-flex-doctable-ram-quota.md | https://github.com/RediSearch/RediSearch/issues/12531 |
| redisearch-flex-tag-compaction-abort.md | https://github.com/RediSearch/RediSearch/issues/12532 |
| arrow-go-cdata-union-import-panics.md | https://github.com/apache/arrow-go/issues/1679 (Component: Other) |
| adbi-difftime-time-binding.md | https://github.com/r-dbi/adbi/issues/130 |
| nanoarrow-r-posixct-int64-double.md | https://github.com/apache/arrow-nanoarrow/issues/1031 |
| sqlalchemy-suite-requirement-gates.md | https://github.com/sqlalchemy/sqlalchemy/discussions/13815 (Usage Questions) |
