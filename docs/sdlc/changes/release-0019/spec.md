# Specification: release and clean installation acceptance

Date: 2026-10-29. Inputs: [intent](intent.md). Execution: [plan](plan.md).

- Use the exact merged/tested source for v0.0.19. Preserve published tags and
  older assets. Build tagged native packages and wheels through release CI.
- Publish the standalone installer plus checksum, three native targets, three
  Python wheel platforms, flat dbc packages and a catalog matching actual assets.
- Publish verified v0.0.19 wheels to PyPI and verify a fresh install includes doctor.
  Do not call the release complete while Python publication remains pending.
- Validate DuckDB 2.0 with a signed scanner and a native driver discovered through
  manifests, without relying on the Redis Python driver package. Use an isolated
  macOS environment and a fresh Linux container with supported glibc.
- Verify initial v0.0.18 installation, fetched SQL results, real upgrade to
  v0.0.19, new-process queries after upgrade, and loss of discovery after uninstall.
  Run Redis Software with active verified 4x QPF and clean up owned test data.
- Validate dbc package installation, native entrypoint/query execution and
  uninstall in separate driver directories. Public registry signing/upload
  remains a Foundry-admin action; reuse the existing onboarding discussion.
- Correct setup commands found stale during the clean test. Keep operational
  artifacts and infrastructure identifiers private.

Evidence: [validation](validation.md). No benchmark or cross-platform performance
claim is part of this work.
