# Validation: installation usability

Date: 2026-10-29. Base: `95d6583`; results apply to the local working-tree change.
Scope: [intent](intent.md), [spec](spec.md), [plan](plan.md).

## Executed checks

- `PYTHONPATH=python /private/tmp/redis-adbc-dagster-venv/bin/python -m pytest -q
  python/tests/test_doctor.py python/tests/test_installation.py
  python/tests/test_driver.py`: **18 passed, 4 skipped**. Skips require REDIS_URI
  (doctor live discovery, existing DB-API/Polars, manifest discovery and collation).
- `python3 -m unittest discover -s scripts -p test_check_wheel_libraries.py -v`:
  **2 passed**, covering the three platforms, wrong architectures and truncation.
- Offline CLI smoke using the existing extracted v0.0.18 macOS library under
  `/private/tmp/redis-adbc-quickstart-library`: native load and entrypoint checks
  passed with manager 1.13.0 and DB-API dependencies. This is not a source build
  or evidence of current Redis connectivity.
- Wrapped that same macOS release library in a temporary wheel-shaped zip under
  the builder's .so member name: the format/architecture validator passed.
- Checked **50 relative link file targets** across setup/integration guides and
  READMEs; parsed all Python setup examples with ast. Passed. Anchors were reviewed
  against the corresponding headings.
- `git diff --check`: passed.

## Limits and review

No running Docker containers/test Redis endpoint were available. Current-source
Go build attempted with temporary caches failed because dependencies were absent
and the sandbox could not resolve proxy.golang.org. No Go source was changed.
Online Search, real manifest discovery, authentication and TLS were not exercised
locally; the new live test joins the existing Python integration CI suite.
No PyPI publication or hosted GitHub workflow was run. No human merge approval
has been recorded. Review corrected the initial false claim about macOS suffixes;
the builder's intentional cross-platform .so wheel filename is retained.

## PR preparation on current main

The original checkout and origin/main have different histories. Applied the scoped
change onto origin/main at 1ed9dad, preserving current CI, Python requirements,
dedicated integration guides and support evidence. Updated the new setup page
to use those current guides and the redis GitHub organization.
