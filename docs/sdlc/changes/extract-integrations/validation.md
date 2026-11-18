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

## Follow-up on PR #489 checks

The documentation check failed on two links to moved Ray tests. Those links and
a stable Airflow heading anchor were fixed. The complete strict MkDocs build
and publication check now pass: 45 HTML pages, links/assets, canonical URLs,
sitemap and search; the replacement hosted documentation check passed.

Focused baseline reproduction on disposable Redis Software 8.8.0 with three
shards, Search and setup-verified active 4x QPF reproduced failures in
`TestSQLAlterMultiAtomic` and `TestSQLFunctionsArgCounts`. Existing default-timing
coverage requires SET DEFAULT typmod errors to be deferred until consumption;
the old atomicity case incorrectly expected definition-time rejection. It now
retains its multi-action rollback assertion using a later failing CHECK, with
new coverage proving that the oversized default fails when consumed, leaves
existing rows intact and permits explicit replacement values. Exact argument
error expectations now include the current parser/function-resolution errors
for normalization, unistr, factorial and session-random functions. No production
Go code changed and no tests were skipped or removed for these fixes.

Before the correction, both focused tests failed. Afterward, those two tests,
the strengthened deferred-typmod regression and the neighboring ALTER/default/
function suites passed through `make -C go test-software` with the same verified
fixture. Other Go packages reported no selected tests; these were focused runs.

[Independent framework CI](https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/actions/runs/44391462558)
passed analytics on Linux amd64/arm64, Airflow, Ray/private-CA worker trust,
helper lifecycle checks and independent wheel/sdist packaging. Superset was
excluded as unsupported, not reported as passing.

The prepared community-registry descriptor is checked into the DuckDB repository.
Creating its PR in the existing registry fork was denied by the connected
GitHub credentials; the old published registry source remains pinned unchanged.
