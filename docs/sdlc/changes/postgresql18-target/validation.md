# PostgreSQL 18 target: validation

Date: 2026-11-09. Source: `08d75d50888374b805a5c89f5bb94a46cf6fe8a9` plus working-tree changes.
Author: Codex. Requestor: Albert Wong. Status: harness checks and live curated comparison completed; compatibility gaps remain.
See [intent](intent.md), [specification](spec.md), and [plan](plan.md).

## Checks completed

- The existing differential-harness unit suite passed: **26 tests**, zero failures.
  Command: `uv run --project go/pgdiff --python 3.14 --frozen python -m unittest discover -s go/pgdiff -p 'test_*.py'`.
  A temporary uv installation/cache supplied the locked dependencies. This suite
  also rebuilt the existing 371-query PostgreSQL corpus and 23 captured dbt cases
  without changes to their source/oracle provenance.
- New reference admission cases accept PostgreSQL 18 with C/POSIX collation,
  reject PostgreSQL 16/17/19, and reject a non-C collation on PostgreSQL 18.
- The official postgres:18 linux/amd64 image digest was resolved with Docker
  buildx; its registry annotation identifies PostgreSQL 18.6.
- `make -C go redis-up` completed. The setup's management-API gate requires an
  active database, active QPF scaling factor 4, six effective Search workers,
  mnp routing and 32 internal connections before returning success. Observed
  Software data plane: Redis 8.8.0, three shards, Search enabled.
- Shared-library build via `make pgdiff` completed with the repository version
  linker flag. The first live attempt failed before comparisons because a
  temporary PATH omitted Docker; it produced no compatibility result. The
  rerun preserves PATH and re-enters the Software/QPF/persistence-policy gates.
- Issue #118 was updated through `gh` outside the sandbox. A fresh read matched
  the prepared title/body exactly and retained the enhancement label. Content
  from the original Measure heading onward, including checklists and migration
  history, was preserved byte-for-byte.

## Live PostgreSQL 18 baseline

The rerun completed against PostgreSQL 18.6 / C collation on the verified
Software fixture: **452/651 scored cases passed (69.4%)**, **199 mismatches**,
**2 allowed extensions**, **2 explicit scope exclusions**, and **0 infrastructure
errors** (655 total cases). The runner returns 1 for differences; make reports
that as exit 2. This run was not an infrastructure failure or a passing suite.

| Tier | Passed / scored |
|---|---:|
| Compatibility issues | 171 / 239 |
| Retained PostgreSQL regression subset | 241 / 371 |
| Captured dbt SQL | 22 / 23 |
| Smoke | 13 / 13 |
| Type checks | 5 / 5 |

[Sanitized aggregate evidence](baseline.json) excludes endpoints, credentials,
raw SQL outcomes and operational identifiers. Full raw reports remain private
in ignored build storage. No SQL implementation changes were made for this run.
The driver revision is the base commit plus the uncommitted reference upgrade.

Local Markdown link checks passed in seven edited/new documents, and
`git diff --check` passed. Historical PostgreSQL 16 artifacts stayed unchanged.

## Evidence limits and review

Live logs, endpoint configuration, credentials and license material remain in
private temporary or ignored build locations. Do not publish raw operational
artifacts. The retained PostgreSQL 16 source corpus is not PostgreSQL 18's full
regression suite. No SQL:2023 Core conformance claim is established.

Agent review: image selection and runtime gate agree on major 18; result/error
comparison, exact allowlists, and retained upstream provenance are unchanged.
Documentation qualifies old measurements and the curated subset. This is an
agent review, not human approval. A fresh upstream PostgreSQL 18 corpus import
and completion of #118's compatibility acceptance criteria remain outstanding.
