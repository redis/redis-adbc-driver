# Validation: v0.0.19 and clean installation

Date: 2026-10-29. Status: native release verified; Python publication awaits PyPI configuration.
Source: `9f18518e1daddd209c23cb58cfadaef07b66fb39`.
Inputs: [intent](intent.md), [spec](spec.md), [plan](plan.md).

## Completed acceptance checks

- PR #242 merged with all build, test, distribution and wheel-client checks
  successful; only its non-publishing upload job was skipped as expected.
- Published [v0.0.19](https://github.com/redis/redis-adbc-driver/releases/tag/v0.0.19)
  with the exact-source `install_driver.py` asset and matching SHA-256. Tagged
  release validation passed and all native/wheel assets uploaded. PyPI publication
  initially stopped on a validator layout assumption; see below.
- Downloaded the published installer and verified its SHA-256 and byte identity
  against the release commit; `python3 -S ... --help` confirmed startup without
  site-packages.
- Fresh macOS ARM64 Python environment: Python 3.15, DuckDB 2.0.0,
  adbc-driver-manager 1.13.0, PyArrow 26.0.0, dbc 0.3.1. No Redis Python driver
  package was present for the native DuckDB tests.
- Fresh Linux ARM64 container: Debian Bookworm/glibc, Python 3.14, DuckDB 2.0.0;
  only DuckDB installed for the native-client test. No Redis Python driver package.
- Both environments installed v0.0.18 native archives through the managed
  installer and loaded Query.Farm's signed scanner from an isolated extension
  directory. Verified driver-name discovery, `SELECT 1`, grouped totals from an
  attached table, and an explicit remote predicate with all rows fetched.
- The clean Linux environment also installed into the default per-user locations
  and resolved `driver='redis'` without `ADBC_DRIVER_PATH`; no host-user driver
  directories were modified.
- The dedicated Redis Software fixture reported active effective 4x QPF, six
  Search workers, mnp routing and 32 connections before testing. This is
  functional installation evidence, not a performance result.
- Installed the real pre-release CI dbc package into an isolated driver directory,
  queried grouped totals through its declared `AdbcDriverRedisInit` entrypoint,
  and uninstalled it. This establishes native loading/querying beyond the earlier
  package-registration-only test; it is not a registry publication claim.
- All three existing dbc packaging tests passed. Corrected the setup guide to
  use Query.Farm's signed DuckDB 2.0 repository and documented the checksummed
  installer release asset.

## Pending completion

- All tagged native build, distribution and wheel-client validation passed.
  Verified the complete set of 27 published assets, checksum metadata and catalog
  URLs for the three available platforms.
- Actual v0.0.18-to-v0.0.19 upgrades passed in the isolated macOS setup and fresh
  Linux container, preserving stable paths and default Linux discovery. Fresh
  DuckDB processes fetched the same expected query results after upgrading.
  Uninstalled both and verified discovery failed in new processes. The owned test
  table was dropped.
- Installed the actual released v0.0.19 dbc archive, verified its manifest version,
  native loading and complete query results, and uninstalled it.
- Posted the exact prepared [Foundry handoff](https://github.com/adbc-drivers/.github/discussions/9#discussioncomment-19283982)
  with the user's explicit authorization. The repository-scoped CLI token could
  not post to that organization; an existing configured discussion-capable login
  succeeded without changing the repository login. The default public registry
  did not return Redis in `dbc search redis`; signing/CDN onboarding remains a
  Foundry-maintainer action.
- Initial PyPI publication rejected the released wheel's valid `.data/purelib`
  location. [PR #243](https://github.com/redis/redis-adbc-driver/pull/243) accepts
  the supported install locations while retaining checksum/platform/binary gates.
  Four regression tests and all three actual released wheels pass the fix. The
  fix merged and all three wheels also passed `twine check`.
- The retry passed artifact validation and reached Trusted Publishing, where
  PyPI returned `invalid-publisher`: no matching publisher for owner `redis`,
  repository `redis-adbc-driver`, workflow `publish-pypi.yml`, environment `pypi`.
  An authorized noninteractive local credential attempt found no configured API
  token and did not upload files. The user was given the exact publishing-settings
  link and fields; publication/fresh-PyPI-install verification remain pending
  that account configuration. No credential material is included in this record.
- The separate release-triggered standalone DuckDB extension build/test workflow
  also passed on macOS ARM64, Linux ARM64 and Linux amd64.
- Documentation checks passed for 51 relative links, code/Markdown syntax,
  whitespace and byte-for-byte equality of the checked-in catalog with the actual
  published release catalog. The isolated Linux container was removed after its
  native installation was uninstalled. No host-user driver directories changed.
- All PR #243 CI checks subsequently passed. The remaining owned Software fixture
  was removed after test data cleanup; private operational files/logs remain
  outside git with access restricted to the current user.

No operational logs, container identities, private endpoints, credentials or
generated certificate material are included here.
