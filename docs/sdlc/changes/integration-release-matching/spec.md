# Specification

Date: 2026-11-26. Requestor: Albert Wong. Source: d65f3b52c9be0a2013711b0716137533f13e9d67. Status: released for driver v0.0.31; community-registry update in review.

The latest `adbc-driver-redis` version on PyPI identifies the latest completed
driver release, because `publish-pypi.yml` runs only after `Release binaries`
succeeds. A `.postN` metadata revision maps to its base version.

- **DuckDB extension:** a daily workflow builds and tests the extension with driver
  tag `vX.Y.Z` on Linux amd64/arm64 and macOS arm64. It verifies every archive's
  checksum, embedded driver revision and version. Only then does it move the
  `driver` pin forward and publish extension release `vX.Y.Z`. It never moves
  the pin backwards and skips released versions. The community-registry `ref`
  is updated separately.
- **Framework integrations:** a daily workflow runs every framework suite against
  the X.Y.Z PyPI wheel. If they all pass, it publishes GitHub release `vX.Y.Z`,
  then uploads the same wheel and sdist to PyPI through Trusted Publishing as
  `redis-adbc-integrations` X.Y.Z, requiring `adbc-driver-redis[dbapi]==X.Y.Z.*`.
  Push, pull request and release CI also test the latest PyPI driver wheel.
- **Nightly:** both repositories build and test this repository's `main`.
- **This repository:** documentation states the matching versions and release
  relationship. It keeps source installation as the route until the first
  `redis-adbc-integrations` release appears on PyPI, and makes no claim that
  any release has been published. No driver code, workflow or SQL behavior changes.

See [intent](intent.md), [plan](plan.md) and [validation](validation.md).
