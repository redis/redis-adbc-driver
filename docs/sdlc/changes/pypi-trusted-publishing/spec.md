# Specification

Date: 2026-10-20. Status: implementation prepared.
See [intent](intent.md) and [plan](plan.md).

Use the existing manually dispatched `publish-pypi.yml` workflow on `main`.
It verifies the stable release, exact platform set, checksums, and wheel metadata
before publishing. Bind PyPI trust to `alberttwong/redis-adbc-driver`, workflow
`publish-pypi.yml`, environment `pypi`. Allow retries with existing PyPI files.
This workflow appeared on main during setup; avoid adding a second upload path.

Acceptance: actionlint, visible publisher registration, live OIDC authentication,
and safe retry of the already published 0.0.15 release. No API token is stored.
