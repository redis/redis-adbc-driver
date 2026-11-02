# Specification

Date: 2026-11-01. Status: implemented locally; full release CI pending.
[Intent](intent.md), [plan](plan.md), [validation](validation.md).

- Every current inventory row has an installation route, execution placement,
  linked setup evidence and remaining constraints. DuckDB is documented separately.
- Add public pandas/Polars extras without changing existing integration dependency
  groups or replacing the managed native install/upgrade/uninstall implementation.
- Existing native/platform/wheel-client validation and the reusable DuckDB workflow
  gate release uploads. Reuse the pinned DuckDB 2.0 source and TLS Enterprise tests.
- Releases attach the existing managed installer/checksum and versioned DuckDB
  extension tarballs containing version/platform metadata and checksums. Native,
  CLI, wheel and dbc packages/catalog remain in the existing pipeline.
- The dedicated PyPI workflow runs after successful release asset upload, reads
  the completed run's tag artifact and excludes PR/foreign/failed workflows,
  unstable tags, draft/prerelease flags and Go test-skip rebuilds. Preserve manual
  retries and metadata-only post-releases.
- Before OIDC publication, validate the exact three-wheel set, package/version,
  checksums and optional GitHub digests. Reuse current native wheel validation,
  including root, .data/purelib and .data/platlib install layouts and CPU format.
- Verified PyPI/GitHub publishing configuration is already correct; no remote
  permissions or settings are changed. First updated workflow execution remains
  separate evidence from account configuration and local tests.
