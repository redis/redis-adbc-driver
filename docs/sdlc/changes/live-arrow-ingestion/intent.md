# Intent

Date: 2026-11-18. Requestor: user. Source baseline: `818063a8`.
Status: implemented and locally validated; [PR #522](https://github.com/redis/redis-adbc-driver/pull/522)
open. Rebased onto `e7c83ad5`; no release implied.

Add Arrow ingestion to live tables: application-owned HASH data with
driver-owned indexes. Append Arrow batches to the registered source collection
without making a managed copy, changing the registration/index, or entering
managed publication. Accept explicit keys or reuse the per-row SQL UUID default
when the input omits `__key`.

This follows [the live-mode PR #510](https://github.com/redis/redis-adbc-driver/pull/510).
See the [specification](spec.md) and [plan](plan.md). New collection creation and
destructive replacement require separate ownership contracts and are excluded.
See [validation and review](validation.md) for evidence and limits.
