# Validation

2026-11-03. Base `bb83527`. Author: Codex. Local verification complete; human review pending.
[Intent](intent.md), [specification](spec.md), [plan](plan.md).

Redis Software fixture: Redis 8.6.2, three shards, TLS, Search enabled. The
repository setup verified active 4x QPF and effective routing/workers. Full
Software tests use the repository noeviction/QPF gate. Operational fixture
files and raw logs remain private.

Focused acceptance tests passed for UUIDs, WITH INSERT, named indexes,
materialized-view lifecycle, sequences, SERIAL/IDENTITY and modifying CTEs.
The first full Software run found an obsolete assertion: it expected ALTER
SEQUENCE to fail at the ALTER dispatch. Updated it to assert the explicitly
unsupported sequence-rename operation. A second run caught a parser regression:
generic ALTER COLUMN SET actions were being consumed as sequence options.
Restricted that path to recognized sequence options, preserving the existing
SET STATISTICS rejection. Both focused regressions now pass.

No complete dbt run, distribution release or cross-platform validation has been
performed for this change. Implementation limits are explicit in the public guide.

Checks completed so far:

- Unit-only Go suite: all five packages passed (`env -u REDIS_URI go test -count=1 -p 1 ./...`). Integration cases skip without REDIS_URI.
- Focused Software regressions: passed, including the function registry, SQLSTATEs, detached ownership, exact int64 bounds, cached concurrent allocation, ingest, failed refresh, and both ALTER regressions.
- Native c-shared library: `make -C go build` passed with the repository version linker flags.
- Change-document relative links and `git diff --check`: passed.

Review findings resolved: schema-name reservation uses watched metadata keys;
SQL index drop keeps physical fields; stage cleanup cannot remove named-index
registrations from the published view; detached sequences survive former-owner
drops; materialized-view writes fail before scanning even when no rows match;
planning-only CTE/schema inspection cannot modify data; adopted noncanonical UUID
text cannot use an unsafe TAG predicate. No human review or approval is recorded.

Final reviewed-code results:

- `make -C go test-software`: **823 top-level tests passed, 40 skipped, zero failures**, across all five Go packages. The driver package completed in 266.134 seconds. Skips are existing ACL/command-introspection checks unsupported by the Software proxy, opt-in profiles and external/environment-specific probes; all added feature regressions ran.
- Focused `-race` Software run: passed for concurrent sequence allocation, DML CTE input images, ingest/ownership/failed refresh, and SQLSTATE/dependency handling. No races reported.
- ADBC validation initially could not start because uv and Python dependencies were absent. Installed uv in a temporary environment and provisioned the test dependencies separately; final outcome follows below.

- Final ADBC run, including `test_sql_schema_additions.py`: **478 passed, 8 skipped, 3 subtests passed**, zero failures, in 20.35 seconds. The native regression verifies SERIAL's Arrow int32, UUID utf8/SQL metadata and v4 values, modifying WITH through the C ABI, named-index filtering, and refresh publication. Skips cover unsupported upstream validation capabilities, including transactions, statistics, table-type filtering, second catalogs and unimplemented ingest modes; no skip was added for this change.

Validation dependencies were installed only in temporary directories. The
repository's ignored uv lock records the resolved upstream validation dependency;
no operational environment, credentials, or raw logs are committed.

The self-review decision is to submit a draft for maintainer review of the
explicit SQL and concurrency limits. This is not human approval or evidence of
complete PostgreSQL/dbt compatibility. Issues remain open.
