# Plan: release and distribution follow-through

Date: 2026-10-29. Inputs: [intent](intent.md), [spec](spec.md).

1. Inspect merged PR #242 checks and current main, release workflows, existing
   dbc packaging/onboarding and published assets. Choose an unused stable tag.
2. Create isolated macOS and fresh Linux environments, install signed DuckDB 2.0
   scanners and v0.0.18 native drivers, and validate complete fetched queries
   against a dedicated verified Software 4x-QPF fixture.
3. Publish v0.0.19 from the tested merge commit with exact-source installer and
   checksum. Await tagged native/wheel validation and upload, then publish PyPI.
4. Complete the real v0.0.18-to-v0.0.19 upgrade/query/uninstall journey and check
   released dbc package/catalog contents and discovery. Prepare the concrete
   existing-discussion handoff for Foundry maintainers.
5. Record actual results and remaining external dependencies; remove owned
   fixtures and correct setup documentation through a follow-up PR.

Public evidence belongs in [validation](validation.md); infrastructure identifiers,
credential-bearing logs and private runtime artifacts stay outside git.
