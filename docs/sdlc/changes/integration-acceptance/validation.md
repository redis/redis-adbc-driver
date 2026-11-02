# Integration acceptance verification

Date: 2026-11-01. Source: `69482fb` plus this working-tree change.
Related: [intent](intent.md), [spec](spec.md), [plan](plan.md),
[50-tool audit and commands](../../../integrations/acceptance.md).

## Local checks

- Built the current native library using `make -C go build`, with a writable
  temporary Go cache. No driver SQL/ADBC implementation changes were made.
- Acceptance runner unit checks: **12 passed**, including audit completeness,
  invalid TLS/configuration, stale evidence, failed/all-skipped/empty/invalid
  reports, continuation, private-CA bootstrapping, and process-group termination
  on timeout. Also passed with Python 3.12, matching the workflow helper runtime.
- CI matrix helper checks: **5 passed**. Software QPF gate checks: **3 passed**
  in the validation environment (the bare host Python lacked `redis`).
- Python compilation, shell syntax, R parsing, workflow YAML parsing and
  `git diff --check` passed. The workflow itself has not run on GitHub.

## Live contract evidence

Used an owned disposable Redis Software 8.6.2 RAM database with Search, one
primary shard, noeviction, authenticated verified TLS and active verified 4x QPF.
The local host is macOS arm64. The current checkout's `.dylib` was loaded; this
does not establish Linux or released-wheel acceptance.

The final sequential Make target produced these JUnit counts (also saved in
[results.json](results.json)):

| Suite | Passed | Failed | Errors | Skipped | Xfailed |
|---|---:|---:|---:|---:|---:|
| SQLAlchemy 2.1.6 | 148 | 81 | 1001 | 140 | 0 |
| DBItest 1.8.3 / adbi 0.1.3 | 6848 | 72 | 34 | 201 | 0 |
| Fivetran SDK 2.13.1 tester | 1 | 0 | 0 | 0 | 0 |
| ADBC validation plus Redis tests | 477 | 0 | 0 | 8 | 0 |

DBI counts are testthat's JUnit entries, including individual expectations, and
are not directly comparable to pytest case counts. Existing upstream skips are
not passes. SQLAlchemy and DBI return failure as intended; neither is certified
as fully compliant. Example SQLAlchemy errors expose PostgreSQL compilation of
unsupported `SERIAL` table columns. DBI failures include type/result assertions,
unsupported operations and limitations in `adbi` (including `field.types`). No
test assertions were weakened to obtain a successful aggregate result.

The first ADBC run lacked the externally verified noeviction declaration and
failed five retained-partition cases. Passing the fixture gate's
`REDIS_RETAINED_CONTROL_PERSISTENCE=guaranteed` resolved all five without changing
their assertions. The Make/workflow paths now execute through that gate.

The initial Fivetran run exposed its SDK 2.13 helper-module relocation. Supporting
the new module location preserved the same SDK tester and warehouse assertions;
the rerun passed. The copied Software CA needed public read permission inside
the container. Package downloads and native test trust also required distinct
process environments; neither TLS verification nor hostname checks were disabled.

The final sequential `make -C go acceptance` completed all four suites and
correctly returned a failing Make status for the SQLAlchemy/DBI findings. Its
sanitized XML/logs and summary are under `go/build/acceptance/` (ignored
operational output). The owned test container and private fixture material were
removed after verification.

## Review and boundaries

Reviewed against `REVIEW.md`: no native SQL/storage changes, no new connector
support claims, no edits to upstream compliance assertions, no dbt/Airbyte suite
duplication, and no credentials intentionally included in uploaded evidence.
Reports redact authenticated Redis URIs/passwords before publication by the
workflow. The workflow uses read-only repository permissions and an ephemeral
runner-owned fixture. Human review and GitHub workflow execution are outstanding.

## PR synchronization, 2026-11-02

Merged main at `0cc1b67` while preparing the PR. Resolved the integration index
conflict by preserving both the acceptance audit and new installation guide.
The container chmod from this branch was initially removed as redundant with
main. A fresh fixture reproduced the readability failure; the follow-up now
sets mode 0644 on the public CA before copying it (the private key stays 0600).
The 12 runner checks,
shell syntax and diff whitespace checks passed again. The live suite counts
above remain evidence from the original source revision; they were not rerun
after this synchronization.


## SQLAlchemy and DBI fixes, 2026-11-02

The baseline above is retained unchanged. The follow-up fixes dialect compilation,
typed parameter conversion, reflection and DBI adaptation, rather than treating
the baseline failures as expected failures. Native NUMERIC division now uses
exact significant-digit arithmetic instead of float64. Cached plans retain
declared types, combined parameter batches resolve a common decimal scale, and
unconstrained decimal results materialize before Arrow schema publication. Empty
results retain the canonical declared fallback; explicitly cast results retain
the same fixed schema whether populated or empty. Dedicated native regressions
cover large/tiny quotients, managed-table fetch and mixed parameter scales.

SQLAlchemy's backend requirements explicitly exclude sequences/autoincrement,
SQL index objects, foreign keys, uniqueness and their unsupported reflection.
Two inherited fixtures omit unsupported index DDL, five unmarked insert cases
receive the autoincrement requirement, and exact cases for reserved metadata
column names and SQL index naming are skipped. Upstream assertions remain intact.
This measures the supported analytics dialect, not full PostgreSQL compliance.

The sourced Redis R helper adds BOOLEAN/BYTEA/TIME/BIGINT/timestamp mappings,
typed and append/overwrite writes, SQL identifiers, temporary-table visibility
and session-aware catalog queries. The dedicated test library installs adbi
**0.1.4** at `ac5d3294597c453c8cb1c278b9536cd9afb3eae2` (verified upstream main
on November 2), with explicit positional-parameter and TIME-binding source fixes.
Nanoarrow **0.9.0** also receives an explicit integer-timestamp conversion fix.
The recipe and patches are in [r/install_acceptance.R](../../../../r/install_acceptance.R).
These results apply to patched dependencies, not the unmodified CRAN packages.

DBI skips its unsupported transaction domain and R-package constructor naming;
a separate assertion verifies transaction rejection and persistent autocommitted
writes. The 14 JUnit skips comprise six upstream Arrow TODOs, five sourced-helper
introspection checks, package naming, unavailable schemas, and the aggregate
transaction domain. DBI JUnit counts include individual expectations.

Final rerun evidence is recorded in [fixed-results.json](fixed-results.json).


The final live Go regression command was `go test -count=1 -p 1 ./...` with
verified fixture settings: all five packages passed (main package 364.795s).
The earlier follow-up run exposed empty-schema and streaming-scale regressions;
those were fixed and covered by `TestSQLDecimalDivisionSchema` and
`TestSQLBoundDecimalDivisionScales` before this successful full rerun. The
fixed-shape empty-result test explicitly casts division to NUMERIC(38,20),
because unconstrained division now intentionally has value-dependent precision.
The window ROUND expectation retains decimal `25.0` rather than float `25`.

Acceptance-runner checks: **13 passed**. CI-matrix checks: **5 passed**.
QPF-gate checks: **3 passed** with their required Python redis dependency.
Python compilation, R parsing, shell syntax, workflow YAML parsing, changed-guide
links and diff whitespace checks passed. Linux/GitHub execution is still separate
from this macOS verification; no released-wheel certification is claimed.


The final sequential acceptance rerun on the rebuilt library passed:

| Suite | Passed | Failed | Errors | Skipped | Xfailed |
|---|---:|---:|---:|---:|---:|
| SQLAlchemy 2.1.6 | 677 | 0 | 0 | 683 | 0 |
| DBItest 1.8.3 / patched adbi 0.1.4 | 9385 | 0 | 0 | 14 | 0 |
| ADBC validation plus Redis tests | 477 | 0 | 0 | 8 | 0 |

Fivetran's 1-pass baseline above was not rerun for these dialect/DBI fixes.


Focused live Python regressions: SQLAlchemy 2.1.6 **5 passed**; SQLAlchemy
1.4.54 / PyArrow 18.1.0 **4 passed, 1 skipped** (the SQLAlchemy 2-only UUID
API case). The owned Software fixture and its generated private connection/TLS
material were removed after verification.
