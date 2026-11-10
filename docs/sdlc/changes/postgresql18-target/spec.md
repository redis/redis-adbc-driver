# PostgreSQL 18 target: specification

Date: 2026-11-09. Source: `08d75d50888374b805a5c89f5bb94a46cf6fe8a9`.
Author: Codex. Requestor: Albert Wong. Issue: [#118](https://github.com/redis/redis-adbc-driver/issues/118).

The runner must select the official PostgreSQL 18 linux/amd64 manifest, reject
other server majors and non-C/POSIX collation, and retain its existing result,
Arrow-type, SQLSTATE, exact-allowlist, and infrastructure-error comparisons.
The official postgres:18 index resolved to an amd64 18.6 manifest on this date:
`sha256:e923a22ac074261dd73970c57edf659475bf0497cb9456f17d98ed7cdd3ca913`.

Retain REL_16_15 inputs, hashes, and captured SQLSTATE oracle unchanged. Input
provenance and live reference version are separate. Importing the PostgreSQL 18
regression corpus, measuring a new pass rate, and implementing dialect gaps are
follow-up acceptance items, not accomplishments of changing the image.

The issue keeps existing child checklist states as historical evidence and adds
explicit PostgreSQL 18 acceptance criteria. SQL:2023 Core conformance and the
three standards-extension issues remain distinct from this compatibility target.

See [intent](intent.md), [plan](plan.md), and [validation](validation.md).
