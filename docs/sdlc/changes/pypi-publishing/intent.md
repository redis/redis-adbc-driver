# Intent: PyPI installation and release publishing

Date: 2026-10-20. Source: `b51d2c8`. Requestor: repository user in this Codex
chat. Author: Codex. Status: implementation and verification. User requested
PyPI publication; no independent human review is implied.
[Specification](spec.md), [plan](plan.md), [verification](validation.md).

Make the Redis ADBC driver installable by package name, including the native
library and optional DB-API dependencies. Verify published wheels and update
installation instructions and package metadata. Upstream
[PR #345](https://github.com/alberttwong/redis-adbc-driver/pull/345) separately
handles automatic release publication; preserve that work.

Version 0.0.15 was already present on PyPI when inspected; all three published
wheel hashes matched the GitHub release. Avoid duplicate uploads or replacing
immutable files. Do not store the API token provided in chat. The existing PyPI
Trusted Publisher points at this repository's `publish-pypi.yml`.

No new platforms, driver behavior, Redis installation or storage changes.
