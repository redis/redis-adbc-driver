# PostgreSQL 18 target: plan

Date: 2026-11-09. Source: `08d75d50888374b805a5c89f5bb94a46cf6fe8a9`.
Author: Codex. Requestor: Albert Wong. Issue: [#118](https://github.com/redis/redis-adbc-driver/issues/118).

1. Inspect upstream regression/conformance resources and existing harness.
2. Update the pinned image and runtime version gate; test wrong-version and
   wrong-collation rejection.
3. Clarify the target and historical provenance in repository documentation and
   issue #118, preserving the legacy checklist.
4. Run harness unit tests and source/provenance checks. Attempt live Software
   setup only through `make -C go redis-up`; require verified active 4x QPF before
   any Redis tests. Record unavailable infrastructure rather than invent results.
5. Track the remaining PostgreSQL 18 corpus import, live baseline, SQL-surface
   inventory, and gap implementation in #118's new acceptance checklist.

See [intent](intent.md), [specification](spec.md), and [validation](validation.md).

Delivered with the mismatch fixes in [PR #366](https://github.com/redis/redis-adbc-driver/pull/366), pending review/merge. The original baseline remains historical evidence; see [subsequent validation](../postgresql18-359/validation.md).
