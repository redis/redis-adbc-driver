# Validation and review

Date: 2026-11-01. Base: `redis/redis-adbc-driver` main, `69482fb`.
Status: local checks and account configuration verified; platform release CI pending.
[Intent](intent.md), [specification](spec.md), [plan](plan.md).

## Current-source checks

- Pulled current organization main before final implementation. The installation
  coverage test matches all **50** numbered inventory entries. Existing managed
  installer lifecycle, dbc packaging and application helpers are retained.
- Python 3.12 publication/coverage suite: **7 passed**. Includes missing/corrupt
  wheels, wrong architecture with valid checksums, draft/prerelease rejection,
  full wheel set and `.data/purelib` layout. Synthetic library headers are used
  for offline gate tests, not as evidence of real Linux builds.
- Existing wheel-format suite: **4 passed**, including root/purelib/platlib
  layouts, ambiguous native files, truncation and mislabeled architectures.
- Existing managed native installer suite: **16 passed**, including install,
  upgrade/uninstall, backup/rollback, tampering and unsafe archives.
- Built the current native library with Go **1.26.10** and macOS 12 deployment
  flags. Built a `macosx_12_0_arm64` wheel with test version `1.2.3`, installed it
  into a temporary Python 3.12 environment, verified `AdbcDriverInit` loads,
  checked all existing helper extras plus pandas/Polars, and created manifests.
- The real current-source macOS wheel passed the extracted publication gate
  alongside synthetic Linux fixture wheels. No test version was published.
- Installed-wheel `python/tests/test_installation.py`: **2 passed**. Total across
  the four focused suites: **29 passed**, no skips.
- `actionlint` for the three changed workflows, `bash -n` for every workflow run
  block, installation-guide local links and `git diff --check`: passed.

## Live configuration

After the user reconfirmed their PyPI password, inspected Trusted Publisher
Management for `adbc-driver-redis`: `redis/redis-adbc-driver`,
`publish-pypi.yml`, environment `pypi` is registered. The historical personal-fork
publisher also remains. Inspected GitHub's corresponding environment: it exists,
with reviewers, wait timer and custom protection unchecked and no deployment
branch/tag restriction. No remote setting or publishing access was changed.

## Review and remaining evidence

Reviewed release upload dependencies, artifact download patterns, publishing
permissions, event/repository/conclusion guards, stable/draft/prerelease/test-skip
selection, checksum and native-layout validation, and preservation of current
installer/dbc/helper behavior. PyPI publication remains in its registered dedicated
workflow; PR builds do not upload release assets or publish packages.

Full Linux/platform/wheel-client and DuckDB build/runtime jobs were not run
locally. Their existing checks remain release gates and must pass in CI. No Redis
data tests were run locally for these distribution changes. No new 50-tool
compatibility claim is made. The workflows must reach `main`, then the first
updated binary and automatic PyPI runs must be observed. Account configuration
and local tests do not establish a completed release. No release was created or
published, and no upstream registry/connector release process was modified.
