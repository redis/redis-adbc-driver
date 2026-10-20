# Verification and review

Date: 2026-10-20. Author: Codex. Status: local checks passed; remote CI and
maintainer review pending.
See [intent](intent.md), [spec](spec.md), and [plan](plan.md).

- Local environment: macOS arm64, DuckDB 1.5.6, disposable Redis Open Source
  8.6.2 with Search on localhost:16389. Driver source baseline e8fddea.
- `python -m pytest -q python/tests/test_duckdb.py`, with RUN_DUCKDB_TESTS=1,
  telemetry opt-out and the disposable URI: 1 passed. This exercises current
  community adbc_scanner reads, joins, ingestion, direct writes, READ_ONLY and
  transaction rejection. Every SQL block in the new upstream Redis guide was
  also executed, fully fetched and cleaned up with a fixture orders table.
- Native extension built with CMake/Ninja and pinned Go 1.26.6; loaded by
  Python DuckDB 1.5.6. `python -m pytest -q duckdb-extension/test`: 16 passed.
  Values and physical Arrow schema match an independently created 10,003-row
  fixture. Checks include LIMIT/empty/count/filter/join behavior, read-query
  guards before connecting, invalid arguments, repeated prepared execution,
  external-access rejection, connection/stream failure cleanup and schema drift.
- The distributable extension with DuckDB statically linked also passed all
  16 integration tests. The dynamic extension compiled against the community
  registry's exact DuckDB 2.0 source commit
  d54cbbe3df17e8e5ce68312f483b4155836112aa, after adapting the binding column
  identifiers, interruption API and extension-link configuration.
- Offline SQLLogicTests on both DuckDB 1.5.6 and the registry's DuckDB 2.0:
  1 test case, 6 assertions passed on each, with
  no skips. The test uses the local extension repository with dynamic loading
  enabled and a temporary extension directory.
- `python duckdb-extension/verify_cli.py` against both DuckDB 1.5.6 and the exact
  DuckDB 2.0 registry build: 12 scenarios passed on each, checking every value
  of 10,003 rows with NULLs,
  Unicode, booleans, decimals and dates. It also checks local count/sum,
  filters/projections, joins, LIMIT/empty scans, SELECT CTEs/set operations,
  rejection of scripts/external access, streaming errors and repeated prepared
  execution. This uses the matching CLI rather than a Python wheel.
- Community registry `scripts/build.py` parsed the single descriptor for
  DuckDB v2.0.0. The descriptor excludes Windows, Wasm and musl and pins the
  public implementation source. Submission:
  [#3157](https://github.com/duckdb/community-extensions/pull/3157).
- [Source PR #344](https://github.com/alberttwong/redis-adbc-driver/pull/344)
  has remote Linux registry-API compilation passing. Broader CI was still
  running when recorded. No unobserved platform results or maintainer approval
  are claimed. The source PR includes native Linux/macOS build checks.

Self-review covers ownership/destruction order, partial initialization,
schema validation, query rejection before connection, TLS URI handling and
external-access checks. No human maintainer review or approval is implied.
The extension reads only; network calls remain subject to driver timeouts.
Historical project-wide Go/ADBC validation has not been rerun for this change.

## CI version-stamp correction

The Linux amd64 native job on source commit 298c67c built the extension but
stamped it for DuckDB v0.0.1: its shallow submodule checkout did not expose the
v1.5.6 release tag. Python DuckDB 1.5.6 correctly rejected that binary before
any integration test could run. Its SQLLogicTests had also silently skipped
because the extension could not be loaded.

The native job now takes its Python package version and explicit
OVERRIDE_GIT_DESCRIBE from one DUCKDB_TEST_VERSION value. The registry API job
also explicitly stamps its pinned source as v2.0.0. The loader's version check
remains enabled. Offline SQLLogicTests now use an explicit build repository
and temporary installation directory, and CI rejects an all-skipped result.
Local reproduction used a depth-one checkout of the pinned DuckDB commit with
no tags (`git describe --tags --long` fails). With the explicit version, the
extension built as v1.5.6 and all 16 integration tests passed against the 1.5.6
wheel. The offline runner passed 6 assertions and the new log guard accepted
that result. Workflow YAML and the Makefile's generated CMake command were
also checked. Verification of the corrected GitHub run is pending.
