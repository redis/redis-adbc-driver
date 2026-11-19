# Intent

Date: 2026-11-19. Requestor: Albert Wong. Author: Codex.
Source: `6f26def0`. Status: documentation update.
See [specification](spec.md), [plan](plan.md), and [validation](validation.md).
PR: [#533](https://github.com/redis/redis-adbc-driver/pull/533). Builds on [implementation PR #532](https://github.com/redis/redis-adbc-driver/pull/532).

Correct the README and ownership comparison to describe live Arrow ingestion as
supported within its write contract. Retain the source-build requirement and
make creation/replacement conditions and partial-write semantics clear.

Follow-up: present the ownership progression as Managed → Live → Unmanaged and
identify Managed as the default directly in comparison tables.
Clarify managed Redis-style access through the existing experimental read-only
native client, separately from ordinary Redis clients and direct physical writes.
Correct stale source-build requirements for NativeClient after checking the
v0.0.29 tag and released wheel; retain source-build scope for newer live tables.
