# Shared snapshot release readiness: verification

Date: 2026-11-10. Source baseline: `018e0368`. Status: v0.0.24 released and verified; all tagged workflows passed.
Issue: [#365](https://github.com/redis/redis-adbc-driver/issues/365).
Related: [intent](intent.md), [specification](spec.md), [plan](plan.md),
[acceptance](acceptance.md), [proposed release notes](release-notes.md).

## Source and environment

The candidate checks below preceded publication. Published source is
`b052eedad9f2fb2c6eeb7c79fe63f8bbc3ca6bdd` (tag v0.0.24); later main-branch
changes are not included in this release. The runtime implementation originated
in merged `018e0368f2f08f5ffd6a51f4978f9d1eae9d8c9b`.
This PR adds the standard native-suite entry point and passes the existing
fixture's selected library/database options to the helper. Candidate artifacts,
certificates and operational environment material remain outside git.

Local macOS arm64, Python 3.15, ADBC manager 1.13.0, PyArrow 26.0.1, Go module
toolchain 1.27.3. Upstream validation harness source:
`f0631651f79bd01b67c5776be6fe3b7fbfbe083b`. Disposable authenticated TLS Redis
Software 8.8.0, Search, noeviction, three primary shards; `software-setup.sh --exec`
verified active 4x QPF and effective Search workers/routing before each Redis run.

## Local checks

- `make -B -C go build VERSION=v0.0.24`: passed. This local version stamp is a
  proposed release candidate, not evidence that v0.0.24 exists on GitHub/PyPI.
- Standard native pytest entry point `validation/tests/test_shared_snapshot.py`
  against the candidate build: one passed, no skips (1.10 seconds).
- Built a local macOS arm64 wheel using `scripts/build_python_wheel.py` and
  installed it into an isolated environment that had no Redis driver package.
  Verified version 0.0.24, library location under the installed package and
  byte-for-byte identity between the installed library and wheel member.
- Complete `validation/tests/` against that installed wheel, selected through
  REDIS_ADBC_LIBRARY rather than the source build: **566 passed, 8 skipped,
  3 subtests passed, zero failures** in 92.71 seconds. The eight skips are
  upstream capability skips: two unimplemented connection cases, unsupported
  table-type filtering, secondary catalog/schema, secondary catalog, two
  unimplemented ingestion cases and transactions. The new shared-view gate
  passed without skips; no allowlist or skip was added.
- Candidate wheel SHA-256:
  `38f5d651a17581d38a3321fcd0257b861c51f5459ddaa17e0e3b603d0ef2e7b7`.
  This identifies only the locally built macOS 26 arm64 candidate, not future
  published platform assets or a release-wide checksum claim.
- Python compile checks and `git diff --check`: passed.
- `scripts/prepare_docs.py`, strict MkDocs build, and `scripts/check_docs.py`:
  passed 42 public pages, local links/assets, canonicals, sitemap, search and
  publication-scope checks.

## Metadata-order failure and correction

The first hosted macOS run failed the new shared-view test's exact Arrow table
comparison (job `130067431542` in run `42884004352`). A local reproduction with
12 schemas failed on its first attempt while reporting equal Arrow schemas and
identical complete content after catalog/schema/table ordering. Driverbase
collects schema callbacks concurrently; that enumeration order is unspecified.
The validator now compares exact Arrow schemas and all metadata after sorting
only the unordered catalog/schema/table collections. It preserves column order,
every field and duplicate entries. An explicit eight-schema native regression
covers this path; no result filter, skip or value tolerance was introduced.
The complete installed-wheel native suite then passed **567 tests, 8 existing
capability skips and 3 subtests, zero failures**, in 152.12 seconds, including
the new multiple-schema regression.

## Hosted checks and remaining gates

PR #374's corrected head `a19cb3f71c7113e6bfd3d2f9708723a0f9469cca` passed
all current-head checks before the user-authorized merge on 2026-11-10.
Merged source: `4d016f150af9d23c4a0f734ab659693a221e9ad6`.

- [Test run 42887182220](https://github.com/redis/redis-adbc-driver/actions/runs/42887182220):
  success, including full Software Go, atomic snapshot, RBAC and native checks.
- [PR packaging run 42887182219](https://github.com/redis/redis-adbc-driver/actions/runs/42887182219):
  success across both Linux architectures, all ten distribution combinations,
  macOS native validation and minimum-client installed-wheel gates.
- [Documentation run 42887182437](https://github.com/redis/redis-adbc-driver/actions/runs/42887182437):
  success.
- Downloaded JUnit reports confirmed both shared-snapshot tests passed without
  skips in each Linux installed wheel and the macOS installed wheel. Each full
  native report recorded zero failures/errors and the same eight capability
  skips. The macOS minimum-client partition report recorded 16 tests, zero
  failures/errors/skips. macOS uses the existing native Redis compatibility
  fixture; it is separate from Software 4x-QPF evidence.

The release-version documentation merged in #377 before the immutable tag was
published. Published-artifact verification follows; PR success alone was not
treated as a publication claim.

The external Airbyte connector was inspected at
`46b4c3c6d8286f017293518630028c48ed256eb8`. Its native backend already scopes one
connection around read discovery/schema planning and table iteration, but the
source still unconditionally rejects shared_snapshot. The public guide names
the integration points and lifecycle changes; no external connector changes or
end-to-end connector enablement are claimed.

Self-review followed REVIEW.md: the new test uses the existing selected library
and endpoint fixtures, the standalone CLI remains compatible, and no runtime,
ACL, storage, retry or transport contract changes. No actionable finding remains
in these local passes. This is agent self-review, not maintainer approval.


## Published v0.0.24 verification

- [Stable GitHub release](https://github.com/redis/redis-adbc-driver/releases/tag/v0.0.24)
  published from `b052eedad9f2fb2c6eeb7c79fe63f8bbc3ca6bdd` on 2026-11-10.
  The tag reference was independently checked against that exact commit.
- [Binary release workflow 42891979077](https://github.com/redis/redis-adbc-driver/actions/runs/42891979077):
  success. Both Linux architectures, all ten distribution combinations, macOS,
  minimum-client wheel gates and all three DuckDB extension jobs passed.
- [Automatic PyPI workflow 42896766559](https://github.com/redis/redis-adbc-driver/actions/runs/42896766559):
  success. [PyPI 0.0.24](https://pypi.org/project/adbc-driver-redis/0.0.24/)
  contains exactly the same three wheel hashes as GitHub. The first normal pip
  lookup briefly preceded simple-index propagation; the normal version-pinned
  install succeeded on retry, without substituting a local candidate.
- Downloaded all 33 published assets and verified SHA-256 sidecars/API digests
  for all 17 non-sidecar artifacts. The repository's release-wheel verifier
  passed the exact three-platform set, versions, archive integrity and native
  library architecture checks.
- Downloaded tagged native JUnit reports: the Linux amd64/arm64 source and
  installed-wheel suites and macOS installed-wheel suite all recorded zero
  failures/errors, with the same eight unsupported-capability skips. Both
  shared-snapshot cases passed without skips in every report. macOS minimum
  client validation recorded 16 tests and no failures/errors/skips.
- Installed the published native macOS archive into an isolated temporary prefix
  using its published checksum. It reported native driver version v0.0.24,
  completed verified-TLS SELECT 1 through both redis/rediss manifests and passed
  the native two-table shared-snapshot validator. Uninstall removed its managed
  files/manifests cleanly; no global installation or manifest location changed.
- Created a fresh Python environment with no Redis driver, then installed
  `adbc-driver-redis==0.0.24` through the normal PyPI index with cache disabled.
  The installed library was byte-identical to both its published wheel member
  and the published native archive's library, and reported native v0.0.24.
  Python 3.15, ADBC manager 1.13.0, PyArrow 26.0.1.
- Against authenticated TLS Software with setup-verified active 4x QPF/Search and
  noeviction, the clean PyPI full-refresh regression passed (1 test, 1.02 seconds).
  Doctor passed native loading, Search/SELECT 1, and both redis/rediss manifest
  discovery/query checks. No source-library override or source Python package
  was used for this installation test.
- [Tagged Test workflow 42891978832](https://github.com/redis/redis-adbc-driver/actions/runs/42891978832):
  success on both architectures, including full Go, atomic snapshot lifecycle,
  authenticated Software RBAC, native ADBC and independent TLS partition checks.

Published wheel SHA-256 values (separate from the earlier local candidate):

| Platform | SHA-256 |
|---|---|
| macOS 15 arm64 | `1d7bae23c2a2f10d2d8c3daecc08b5335ef340f713284c3d6a2157113f4deb01` |
| manylinux 2.28 arm64 | `2eac708ccb04643621e36c53e3981407a7e2b117b286a4e34766b219c9fee2c7` |
| manylinux 2.28 amd64 | `ba9f14bae231c57c39c2e96bccf6683dc36fc576f5b530032843fed71329d8b0` |

Release authorization was the user's explicit approval in this chat to merge the
passing readiness change and publish v0.0.24. No additional formal GitHub review
is inferred from that authorization. The current external Airbyte connector still
requires the documented native integration; Flight SQL remains unsupported.
