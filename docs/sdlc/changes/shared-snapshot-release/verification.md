# Shared snapshot release readiness: verification

Date: 2026-11-10. Source baseline: `018e0368`. Status: local candidate validated; hosted release and publication pending.
Issue: [#365](https://github.com/redis/redis-adbc-driver/issues/365).
Related: [intent](intent.md), [specification](spec.md), [plan](plan.md),
[acceptance](acceptance.md), [proposed release notes](release-notes.md).

## Source and environment

No release is claimed. Runtime source is merged `018e0368f2f08f5ffd6a51f4978f9d1eae9d8c9b`.
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

## Hosted checks and remaining gates

PR #373 merged as `018e0368`. Documentation, helpers, coverage and Superset jobs
passed. At this record's update, the amd64/arm64 Software jobs were still running;
no hosted completion or release-platform success is claimed from that snapshot.
Binary and PyPI release workflows, published asset checksums and a clean install
from published artifacts remain required before a release is claimed.

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
