# Intent: refresh Software Flex documentation

Date: 2026-11-05. Source: main `43f4f061`. Author: Codex, at the user's request.
Status: documentation prepared for review. See [spec](spec.md) and [plan](plan.md).

## Problem and desired outcome

The README still labels Flex unsupported and TRUNCATE unavailable, while the
stopgap guide describes restart/HA and resource measurements as pending. The
implementation/evidence already merged in [PR #323](https://github.com/redis/redis-adbc-driver/pull/323)
and [PR #331](https://github.com/redis/redis-adbc-driver/pull/331).

Help users identify the opt-in Software Flex scope, latest recorded validation,
remaining failures and meaning of the performance averages. Related trackers:
[#314](https://github.com/redis/redis-adbc-driver/issues/314) and
[#322](https://github.com/redis/redis-adbc-driver/issues/322).

## Constraints and exclusions

Preserve dated raw results and failed checkpoints. Keep #325/#326 intentionally
skipped and Cloud #329 excluded. This request changes documentation; it does not
add an ALTER/index implementation, a new deployment or new benchmark results.
