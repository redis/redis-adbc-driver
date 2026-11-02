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

## PR CI follow-up

The first PR run exposed two existing fixture/workflow mismatches:

- [Linux integration installation](https://github.com/redis/redis-adbc-driver/actions/runs/41393123229/job/125108644964)
  stopped before tests: uv's Python download failed with `UnknownIssuer` because
  the Redis fixture's `SSL_CERT_FILE` replaced public download trust roots.
  Reproduced with uv 0.13.6 against PyPI using a temporary private CA (exit 2),
  then resolved the same pinned package with that CA plus a public-root
  `SSL_CERT_DIR` (exit 0). Linux jobs and distro validation containers now expose
  the installed public CA directory alongside the unchanged Redis CA. No TLS
  verification is disabled.
- [macOS release Go test startup](https://github.com/redis/redis-adbc-driver/actions/runs/41393123355/job/125108610667)
  rejected the native fixture's plaintext URI under the current authenticated-TLS
  test policy. The existing native fixture now starts with `--tls`, its generated
  authenticated URI/CA are exported without logging credentials, and both cap
  configuration commands use verified TLS. Its public-root directory also keeps
  uv downloads working. Fixture private material is removed during cleanup.

The Go harness startup check (`go test -run '^$' .`) reproduced rejection of
the old plaintext fixture (exit 1) and accepted the generated authenticated TLS
URI/CA (exit 0, no data tests selected). This checks policy setup only.

Actionlint, every changed shell block's `bash -n`, and `git diff --check` passed.
A replacement CI run is needed for the full integration and release gates; local
TLS trust and test-policy checks do not establish full server/runtime success.

The next run reached Go tests and exposed two unit-test assumptions about an
unset `SSL_CERT_FILE`: `TestClientURIOptions` intentionally parses `skip_verify`,
and `TestExplicitTLSRoots` asserts that clearing its explicit option returns to
native trust. Both now use `t.Setenv` to isolate that setting and restore it for
subsequent integration tests. No security assertion or test selection changed.
Reproduced both failures locally with the generated fixture CA, then verified the
two tests pass. The complete unit-only Go suite also passed with the inherited CA
(`REDIS_URI` unset, `-count=1 -p 1`); live integration tests skipped in that run.

Subsequent CI completed Linux Go, ADBC, wheel, partition and reflection checks,
then rejected the Software proxy TLS handoff: the helper assumed a plaintext
initial database and the release step requested one shard, while current
`redis-up` provides authenticated TLS and three shards. The handoff now requests
three shards and accepts the owned fixture's TLS-enabled initial state, preserving
port/name/role ownership checks and verified TLS/QPF/RBAC validation after setup.
A new regression test failed before this fix and passed afterwards; wrong shard
counts, unknown TLS states and existing role assignments still fail before
certificate changes. The Software management suite passes all four tests.

A macOS run then reported a broken pipe in the wide-row fetch integration test.
The exact test passed five consecutive runs against an isolated native macOS
Redis 8.10.3/Search 8.10.1 TLS fixture obtained from official Homebrew bottles.
No retries or skips were added to CI; a redacted server-log diagnostic is now
printed on macOS failure so later failures can be diagnosed without relying on
artifact downloads. Full-suite reproduction and replacement CI remain required.
