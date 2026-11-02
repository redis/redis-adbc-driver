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
Main now fixes public CA readability before copying it into Software, so the
redundant container chmod from this branch was removed. The 12 runner checks,
shell syntax and diff whitespace checks passed again. The live suite counts
above remain evidence from the original source revision; they were not rerun
after this synchronization.
