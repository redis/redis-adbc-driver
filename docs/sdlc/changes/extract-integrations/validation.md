# Validation

2026-11-18. Local checks passed:

- Core integration registry/runner contracts: 11 tests.
- Parallel CI isolation and gate contracts: 4 tests.
- Core distribution and wheel publication contracts: 8 tests.
- Upstream acceptance runner contracts: 13 tests.
- CI matrix selection: 9 tests; apt retry shell checks: 4 tests.
- Moved Ray TLS ownership/failure cleanup/report accounting: 6 tests.
- Integration wheel/sdist build and namespace/package-boundary check.
- actionlint on changed core and both destination workflows; Python compilation;
  git diff whitespace checks.

These are offline checks, not live Redis compatibility evidence. Destination
CI and the driver removal PR will provide live Software 4x QPF results. No
ClickBench workload was run; historical benchmark data was not relabeled.

Superset 6.1 remains unsupported on Python 3.14+. PyPI publication and a new
DuckDB community registry descriptor are not implied by repository extraction.
