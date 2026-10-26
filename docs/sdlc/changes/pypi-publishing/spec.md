# Specification

Date: 2026-10-20. Source: `b51d2c8`.
[Intent](intent.md), [plan](plan.md), [verification](validation.md).

- Document `pip install 'adbc-driver-redis[dbapi]' polars` for supported
  platforms. Redis and Search remain external prerequisites.
- Preserve existing v0.0.15 wheels and verify their hashes against both
  GitHub release assets and PyPI metadata before claiming availability.
- Future wheel builds include a Markdown description and repository/docs/issue
  URLs. The builder continues stamping the release tag's version.
- Preserve existing publishing workflows and external publisher configuration.
  No credentials are stored and no duplicate files are uploaded.

Validate with clean PyPI installation, native loading and existing driver smoke
tests on disposable Redis. Build an unpublished local wheel and validate its
description with `twine check --strict`.
