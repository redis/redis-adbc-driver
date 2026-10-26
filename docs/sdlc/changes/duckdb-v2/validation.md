# Verification

Date: 2026-10-20. Author: Codex. Status: local checks passed; remote CI and
maintainer review pending.
See [intent](intent.md), [spec](spec.md), and [plan](plan.md).

The maintainer's comment and current community build workflow confirm that
repo.ref targets 2.0 at d54cbbe3df17e8e5ce68312f483b4155836112aa, using
v2.0-cyanoptera extension tools. Source submodules now match those pins.

On macOS arm64, the extension was rebuilt with EXTENSION_STATIC_BUILD=ON
against that exact 2.0 engine and loaded by its matching CLI. A disposable
Redis 8.6.2 with Search on localhost:16389 provided uniquely named fixture
tables, which the harness cleaned up.

- `python duckdb-extension/verify_cli.py`: 13 scenarios passed. Physical types
  were BIGINT, VARCHAR, BOOLEAN, DECIMAL(12,2), DATE; every value in 10,003 rows
  was checked, including NULLs, Unicode, decimals and dates. The existing local
  query, prepared execution, read-guard and error scenarios passed as well.
- The pinned 2.0 SQLLogicTest runner reported 1 passed, 0 skipped using the
  local extension repository and a temporary installation directory.
- A deliberately missing repository/installation directory produced 0 passed,
  1 skipped but still exit status 0 despite --fail-require-skip. The pinned
  upstream runner checks a different output stream for that skip. CI therefore
  also validates the final counts. The exact summary predicate accepted the
  good run and rejected the missing-extension run.
- Python syntax, workflow YAML, Makefile command expansion, pinned source/tool
  versions and `git diff --check` were checked. The Makefile selects the 2.0
  runner with --fail-require-skip; the workflow adds the strict summary check.

The local build reused a checkout of the same pinned 2.0 source and omitted
unrelated built-in extensions; the new native CI performs the standard full
build on Linux amd64/arm64 and macOS arm64. Remote results are pending.
No maintainer approval, deployment or unobserved platform success is claimed.
