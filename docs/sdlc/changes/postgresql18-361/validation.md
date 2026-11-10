# PostgreSQL 18 join and whole-row validation

Date: 2026-11-10. Author: Codex. Requestor: Albert Wong.
Status: targeted implementation validated; five #361 cases remain unresolved.
Source: base `85509485dbc07b2e2e0ea9e1b8e08bd11a290587` plus the working-tree
files identified by SHA-256 in [sanitized results](results.json).
See [intent](intent.md), [spec](spec.md), [plan](plan.md) and
[issue #361](https://github.com/redis/redis-adbc-driver/issues/361).

## Result and scope

The unchanged, fully fetched corpus reports **589/651 scored passes (90.48%)**,
**62 failures**, **two allowed extensions**, **two explicit exclusions** and
**zero infrastructure errors** (655 total cases). All **574 previously passing
cases remain passing**. The corpus and allowlist were not changed.

The reference is the pinned PostgreSQL **18.6**, linux/amd64 image with C
collation. Redis is **Redis Software / Redis 8.8.0**, three shards, with active
**4x QPF verified before each live command** by `software-setup.sh` and its
management gate: six effective Search workers, mnp routing, 32 connections,
active primary shards and noeviction. A dedicated disposable Compose fixture
avoided a TLS trust conflict on the shared fixture. The first failed startup
ran zero corpus cases and is not compatibility evidence.

All eleven retained joined-table/USING-alias cases and all three whole-row
reference cases now pass. The existing `issue-254.json:row-10` (`row_to_json`)
also passes, bringing the increase to **15 cases**. The retained #359 inventory
now has **137 observed passes and 51 failures**; #360's original eleven failures
remain failures. The complete corpus still returns mismatch exit 1 (`make`
reports exit 2). This is a pass rate above 90%, not an all-passing suite.

The corpus retains REL_16_15 source provenance and is compared with the live
PostgreSQL 18 reference. This is neither the full PostgreSQL 18 regression suite
nor a measure of complete PostgreSQL feature coverage or SQL conformance.

## Implemented behavior and review

- Parenthesized joins execute as operands with their own association, column
  aliases and public star order. A whole-join alias hides inner names; a USING
  alias exposes only its merged columns without duplicating unqualified input.
- Group execution is deferred until scanning, preserving planning-only/empty
  behavior and cancellation. Qualified imported-key provenance survives groups;
  it remains outside public stars and whole-row fields. Retained execution
  explicitly rejects grouped joins pending its separate admission support.
- A missing-column lookup can resolve to a visible whole relation, while real
  columns and ambiguity errors retain precedence. Anonymous query records retain
  field names and types, including duplicate names, without becoming storable
  Arrow-ingested structs. Presence markers distinguish an absent outer-join row
  from a present all-NULL row, including correlation and aggregate representatives.
- Qualified row stars rebind when the input schema changes and expand by field
  position. Regression cases cover duplicate field names, row comparisons,
  NULLs, grouping, nested groups and public Arrow column labels.
- Anonymous composite comparison uses independent PostgreSQL binary field OIDs
  and values, and reads Arrow struct children by position. Checks reject changed
  values, field counts and integer widths; NULL composites stay distinct from
  composites with NULL fields. The binary wire has no anonymous field names;
  SQL/Arrow regressions check names separately. Named PostgreSQL composite OIDs
  are not silently equated with anonymous records.
- Binary reference decoding preserves temporal bounds/fractions, interval
  components, JSON strings and regclass/regtype display names. Catalog names
  are resolved through PostgreSQL's own catalog/search_path after fetching,
  without rerunning the original query or using Redis output as an oracle.

Review followed the correctness/lifecycle, ownership/security and evidence
passes in [REVIEW.md](../../../../REVIEW.md). The broader Software run caught a
provenance RETURNING-label regression; it was corrected before the final run.
The old wrapped-empty-schema fixture expected Arrow NULL for an unknown NULL
output. An independent PostgreSQL 18 query confirmed OID 25 (text), including
`WHERE false`; the expectation now checks UTF-8 for the wrapped relation while
retaining the existing standalone schema check. No assertion was removed and
no skip or allowlist was broadened. A final review also identified that scalar parallel reducers must reject
whole-row arguments; these now use expression evaluation, with a COUNT(t)
admission/value regression. Agent review is not maintainer approval.

## Checks

- `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...`: passed. Live tests
  skip without REDIS_URI; this is unit evidence, not live compatibility evidence.
- `make -C go pgdiff-test`: **35 tests passed**. Provenance import regenerated
  the same **371 PostgreSQL** and **23 dbt** queries.
- Through `software-setup.sh --exec`, the broader relevant join, LATERAL,
  subquery, grouping, record, recursion, empty-query and provenance selection:
  **63 top-level tests passed**, one failed on the old wrapped-schema expectation.
  The final rerun covers that correction, new join/whole-row tests, imported
  provenance, record behavior and MERGE: **17 top-level tests passed**, zero
  failures or skips. The full
  default Software suite was not run for this change.
- Through the same verified gate, `make pgdiff
  PGDIFF_ARGS='--report build/pgdiff-361-final.json'`: final counts above.
- `python3 go/pgdiff/check_issue359.py go/build/pgdiff-361-final.json`: expected
  exit 1, **137/188 retained passes and 51 unresolved failures**.
  `--triage` exits 0 for complete accounting and explicitly preserves failures.
- `git diff --check` and local Markdown link checks: passed.

For reproduction, use `make -C go redis-up` and run the live commands via
`bash software-setup.sh --exec` from `go/`. For concurrent work, supply a dedicated
Compose project and distinct Software API/database ports to both setup and test
commands. The final regression selection is:

```sh
bash software-setup.sh --exec go test -count=1 -v -p 1 -timeout=30m \
  -run '^(TestGroupedJoin.*|TestWholeRow.*|TestQualifiedStarsRetainPublicNames|TestSQLEmptySchema|TestSQL.*(Record|Merge).*|TestManagedImportProvenanceJoins)$' ./...
```

The dedicated disposable Software fixture was removed after validation.
Operational logs, raw reports, endpoints, credentials and infrastructure names
remain private in ignored build or temporary storage.

## Remaining #361 cases

| Stable case | Remaining behavior |
|---|---|
| `dbt/utils.json:event-0207-0` | Logical text identity through expressions/CTAS into information_schema |
| `postgres/case.json:statement-0020` | Planner constant folding with dead-branch and volatility rules |
| `postgres/window.json:statement-0041` | Exact NUMERIC EXTRACT, distinct from DATE_PART, through CASE/arithmetic/windows |
| `postgres/with.json:statement-0010` | Demand-driven recursive UNION ALL consumer limit |
| `postgres/with.json:statement-0011` | Demand-driven recursive UNION consumer limit |

These remain scored failures. This change does not close #361 or #118.
