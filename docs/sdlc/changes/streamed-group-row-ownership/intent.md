# Intent

Date: 2026-11-24. Status: optional local candidate; native validation pending.
Origin: grouped streaming from [PR607](https://github.com/redis/redis-adbc-driver/pull/607),
related to [TPC-DS Q4 issue590](https://github.com/redis/redis-adbc-driver/issues/590).
See [specification](spec.md), [plan](plan.md), [verification](verification.md).

The coordinator clones a new group's representative whenever metadata is memory
backed, even when a proven star stream just emitted a fresh owned leaf map.
Transfer that leaf map directly after aggregate inputs have consumed it. Preserve
clone-before-prune for every shared, refused, private or retained memory path.
The independently recorded [panic lifecycle correction](../parallel-star-panic-join/intent.md)
keeps callback unwinding safe; that fix remains separate from this optional allocation change.
