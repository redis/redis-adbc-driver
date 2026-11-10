# Proposed PostgreSQL 18 query compatibility profile

Date: 2026-11-10. Status: proposal; no complete profile is published or passed.
Parent: [#118](https://github.com/redis/redis-adbc-driver/issues/118).

“PostgreSQL 18 Query Profile v1” is a working name for a project-defined,
versioned contract for selected query behavior through ADBC. It is not an
official PostgreSQL certification. This proposal records the next milestone;
it does not declare its candidate constructs supported or create a test suite.
The broader PostgreSQL 18 dialect objective remains open in #118.

## What the profile must define

A published profile needs all of the following:

1. **An exact scope and manifest.** Name the SQL constructs, types, functions,
   overloads, limits and stable test IDs. Candidate areas are SELECT expressions,
   predicates, NULLs, sorting/pagination, aggregates, selected joins, CTEs and
   windows. Decide inclusion from product requirements, not by selecting only
   examples that already pass. A passing example does not certify an entire
   function or query construct.
2. **A controlled reference.** Pin PostgreSQL 18 and its image/source revisions,
   encoding, collation, time zone and session settings. Define fixture loading
   independently of the query contract. Preserve upstream provenance and explain
   adaptations; retained REL_16_15 inputs are not PostgreSQL 18 source tests.
3. **Observable ADBC semantics.** Specify fully fetched values, duplicates,
   NULLs, ordering, Arrow schemas, precision/range mappings, parameter binding,
   specified SQLSTATEs, and relevant fetch/cancellation behavior. Justify exact
   comparison/normalization rules and test that real differences are rejected.
   ADBC's interface alone does not supply this PostgreSQL dialect contract.
4. **Behavioral coverage.** Include positive, negative, error, empty/all-NULL,
   boundary and applicable multi-connection cases for every declared behavior.
   Reading a result schema or starting execution without completing the fetch
   is not evidence of matching results.
5. **Explicit exclusions and owners.** State unsupported syntax, overloads,
   representations, precision/range limits and storage/security behavior.
   Exclusions qualify the claim; they are not passing tests. Multi-statement
   transactions, row/table locking, integrity enforcement and PUBLIC security
   semantics remain separately tracked work.
6. **A versioned completion gate.** Every published profile test must pass, with
   zero unresolved profile mismatches and zero infrastructure errors. Keep the
   broader corpus and its failures visible. Do not widen allowlists, silently
   drop cases, or change an expected answer to obtain a profile pass. Review and
   version changes to the scope, exclusions, mappings and test manifest.
7. **A reproducible report.** Record the driver revision, exact profile version,
   reference, server configuration, provenance, pass/fail/skip/exclusion counts
   and evidence limits. Use Redis Software with active, verified 4x QPF. Keep
   credentials, endpoints, license material and operational identifiers private.

After this milestone is actually met, a precise claim would be:

> Passes all tests in the published PostgreSQL 18 Query Profile v1 through ADBC
> under the documented configuration.

That claim identifies the evidence and its limits. It does not establish all
possible behavior within PostgreSQL, a pass of its full regression suite, or
SQL:2023 Core conformance. The manifest and its implementation are still work
items, not delivered by this documentation proposal.

## Current evidence and categorized failures

Merged [PR #366](https://github.com/redis/redis-adbc-driver/pull/366) delivered the
#359 fix-or-classify triage. At measured implementation revision `b35a4739`, the
curated comparison reported **574/651 scored passes (88.2%)**, **77 failures**,
two allowed extensions, two explicit scope exclusions and zero infrastructure
errors. The 655-case corpus includes DDL/DML and storage-model cases as well as
queries; it is not yet the proposed query-only profile. This is not a new run of
the merge commit or a release claim.

The [#361 join/whole-row follow-up](sdlc/changes/postgresql18-361/validation.md)
measured **589/651 passes (90.48%)**, **62 failures**, two allowed extensions,
two explicit exclusions and zero infrastructure errors on Redis Software with
verified 4x QPF. Its source-content manifest accompanies the result; this is a
working-tree measurement, not a release claim. It fixes fourteen #361 cases and
one existing `row_to_json` case from #98. Five #361 cases remain open.

The [#98 JSON follow-up](sdlc/changes/postgresql18-json/validation.md) measured
**597/651 passes (91.71%)**, **54 failures**, two allowed extensions, two explicit
exclusions and zero infrastructure errors on Redis Software with verified 4x QPF.
All 589 previous passes are retained; the eight remaining JSON corpus cases pass.
The broader JSON issue remains open. Separate adversarial cases are not included
in this denominator.

The latest 54 failing cases are grouped below. Counts are stable test cases, not
numbers of distinct features, issues or mandatory requirements.

| Category | Failing cases | Owning issues |
|---|---:|---|
| Query semantics and metadata | 5 | [#361](https://github.com/redis/redis-adbc-driver/issues/361) |
| Numeric/temporal representation | 24 | [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| JSON types, operators and functions | 0 | [#98](https://github.com/redis/redis-adbc-driver/issues/98) |
| Comparison rules and fixture scope | 7 | [#364](https://github.com/redis/redis-adbc-driver/issues/364) |
| Transactions, locking and integrity | 12 | [#360](https://github.com/redis/redis-adbc-driver/issues/360), [#113](https://github.com/redis/redis-adbc-driver/issues/113), [#114](https://github.com/redis/redis-adbc-driver/issues/114), [#116](https://github.com/redis/redis-adbc-driver/issues/116) |
| PUBLIC privileges | 2 | [#363](https://github.com/redis/redis-adbc-driver/issues/363) |
| Enum types | 2 | [#107](https://github.com/redis/redis-adbc-driver/issues/107) |
| Remaining functions/operators | 2 | [#110](https://github.com/redis/redis-adbc-driver/issues/110) |
| **Total** | **54** | |

#359 retained 188 baseline failures: 122 now pass and 66 were explicitly
deferred. #360's original eleven cases also still fail. The storage category
above includes those eleven plus the retained unique-index case, counted once.
Closing #359 completed triage, not all deferred feature implementations.
After the #361 follow-up, 137/188 retained cases pass and 51 remain deferred;
the original eleven #360 cases remain failures. After the JSON batch, 145/188
retained cases pass and 43 remain deferred.

The remaining retained 43 have a separate classification: five Core-related,
four optional-standard-related, three PostgreSQL-specific, twenty-four
ADBC representation/comparison limitations and seven fixture/oracle cases.
That classification does not cover all original eleven storage cases or count
mandatory features. “Core-related” identifies an association; exact standard
subfeature mapping remains unverified where recorded.

See the [stable case audit](sdlc/changes/postgresql18-359/cases.md),
[validation record](sdlc/changes/postgresql18-359/validation.md),
[sanitized results](sdlc/changes/postgresql18-359/results.json), and
[harness documentation](../go/pgdiff/README.md). Future profile exclusions must
not remove these broader failures from #118's accounting.

## Separate targets and reading material

| Target | Meaning |
|---|---|
| This proposed query profile | A project-defined contract and versioned suite for declared query behavior through ADBC. |
| PostgreSQL's full regression suite | A finite implementation suite covering standard SQL and PostgreSQL extensions. |
| Full PostgreSQL 18 compatibility | A much broader behavior promise, extending beyond the cases exercised by a suite. |
| All 177 SQL:2023 Core features | Mandatory standard requirements needing a separate requirement-by-requirement audit and evidence. |

These are not strictly nested targets. PostgreSQL's own supported/unsupported
feature inventory prevents using PostgreSQL as the sole positive oracle for
every mandatory standard requirement. Completing #356–#358 does not establish
all 177 for this driver, and excluding transaction/integrity/privilege behavior
from a query profile cannot establish Core conformance.

Research on 2026-11-10 did not identify a maintained public suite verifying all
177 SQL:2023 Core features. This is a research finding, not proof that no private
or commercial suite exists. A future Core effort needs a matrix against the
normative SQL:2023 requirements and independent expected behavior where the
PostgreSQL oracle is unsupported or differs from the standard.

- [PostgreSQL 18 regression tests](https://www.postgresql.org/docs/18/regress.html)
  cover standard operations and PostgreSQL extensions.
- [PostgreSQL SQL conformance](https://www.postgresql.org/docs/18/features.html)
  and its [feature inventory](https://www.postgresql.org/docs/18/features-sql-standard.html)
  explain the standard feature assessment. The
  [sql_features view](https://www.postgresql.org/docs/18/infoschema-sql-features.html)
  documents that PostgreSQL does not perform formal feature-conformance testing.
- [NIST SQL Test Suite 6.0](https://nvlpubs.nist.gov/nistpubs/Legacy/IR/nistir5998.pdf)
  is legacy SQL-92 coverage. Reuse requires an applicability audit; it cannot be
  relabeled as a SQL:2023 Core verifier.
- [sqllogictest](https://sqlite.org/sqllogictest/doc/trunk/about.wiki)
  checks query results and excludes transactional behavior, concurrency and
  locking from its scope.
- [ADBC API Standard](https://arrow.apache.org/adbc/current/format/specification.html)
  describes the interface for statements, Arrow results, metadata and errors;
  its overview points to adbc.h as the canonical API definition.
