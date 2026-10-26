# Verification and review

Date: 2026-10-20. Source: `b51d2c8` plus this change. Author/reviewer: Codex;
agent review is not independent human approval.
[Intent](intent.md), [specification](spec.md), [plan](plan.md).

## Published artifact evidence

At inspection, [PyPI 0.0.15](https://pypi.org/project/adbc-driver-redis/0.0.15/)
already contained all three release wheels. No token was used, stored or uploaded
by this change, and no duplicate publication was attempted. Downloaded the
[v0.0.15 release](https://github.com/redis/redis-adbc-driver/releases/tag/v0.0.15)
wheels and checksum files; each SHA-256 matched both the release checksum and
PyPI JSON metadata:

- macOS arm64: `499bd20fb3cd7f2f0a534c99ba05389325c58ba6760940bd1f0c20f88a152b1c`
- Linux arm64: `a39656952604e25f1d650f1e5eda5cd89a5bafa5e75cf7d59f095b9ab0e7009a`
- Linux x86_64: `3a4b0da331e78447ada5bc8ce0bc3fd0616ca85e058b2c55401deb038be1ee7c`

The existing release [build run](https://github.com/alberttwong/redis-adbc-driver/actions/runs/39470185455)
completed successfully. This is existing release evidence, not execution of the
new publication attempt.

## Checks performed

- Created a clean Python 3.12 virtual environment and installed
  `adbc-driver-redis[dbapi]==0.0.15`, Polars and pytest using the PyPI index.
- Against a disposable `redis:8.6.2` container on loopback port 26382, ran
  `python -m pytest -q python/tests/test_driver.py`, with `REDIS_ADBC_LIBRARY`
  and `PYTHONPATH` unset: **4 passed**. Tests cover the DB-API and Polars URI
  paths, manifest discovery, autocommit rejection and collation.
- Confirmed package imports resolve inside the clean virtual environment,
  version is 0.0.15, and the bundled native library exports `AdbcDriverInit`.
- Built an unpublished local metadata-validation wheel with the existing native
  library and a synthetic next version (0.0.16); `twine check --strict` passed.
  Local Go 1.27's native library requires macOS 13, so this check uses the
  `macosx_13_0_arm64` tag. Release CI still builds for macOS 12 using Go 1.26.
- `git diff --check`: passed.

## Review and limits

Reviewed package metadata, credential exposure and installation prerequisites.
No driver runtime or storage code changed. The original release wheels lack a
long description (`twine check` warns); the package README and metadata added
here apply to future builds and were verified with strict checking.

The existing PyPI Trusted Publisher is configured for this repository's
`publish-pypi.yml` workflow and `pypi` environment. Neither it nor repository
publishing workflows were changed here. Automatic release publication is tracked
separately in PR #345. Linux/macOS release build matrices were not rerun locally;
this change's live install test is macOS arm64.
