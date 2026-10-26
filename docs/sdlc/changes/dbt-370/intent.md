# Intent: recognize Redis dbt v1 and v2 adapters

Date: 2026-10-21. Source: `95d6365bb76212a49888bfec423cf8c8b15e61a7`.
Requestor: repository owner, via chat. Author: Codex. Status: proposed for review.
Issue: [#370](https://github.com/alberttwong/redis-adbc-driver/issues/370).

## Problem and outcome

Row 7 incorrectly says a Redis dbt adapter is missing. Recognize the community
v1 adapter and experimental v2 implementation, with their validation evidence.
Consumers need to distinguish installable Core 1.x support from a custom Rust
engine build. Flight SQL implementation, upstream engine inclusion and new
integration tests are outside this documentation change.

## Handoffs

See [spec](spec.md) and [plan and evidence](plan.md). No human approval is inferred.
