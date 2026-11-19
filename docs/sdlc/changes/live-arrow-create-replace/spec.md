# Specification

Date: 2026-11-18. Status: implemented; local validation passed.
See [intent](intent.md), [plan](plan.md) and [validation](validation.md).
PR: [#532](https://github.com/redis/redis-adbc-driver/pull/532).

- Statement `adbc.redis.ingest.live_source` accepts strict JSON prefix, optional
  encodings, and replacement-only current index and consent. Require enabled
  live tables, redis_live, permanent top-level execution, no retained/shared
  operation, and no unmanaged_source. Preserve existing append behavior.
- Create/create_append creation derives a bounded live definition from Arrow,
  requires an empty application prefix, reserves a generated index identity
  create-only before FT.CREATE, waits for readiness and publishes ready by CAS.
  No Arrow Next before registration. Conflicts never retarget input. Failures
  retain registration/index recovery identity and may leave partial source rows.
- Replacement requires exact schema/table/prefix/current index and true
  exclusive_quiescent, discard_ttls and reset_index attestations. Existing
  dedicated-source overlap/consumer/permission preflight and bounded inventories
  apply. Refuse aliases/nested/non-HASH data; delete only unchanged inventoried
  images/expiry. Drop index without DD, recreate the live schema/index policy,
  stage the replacement schema by CAS before index creation, publish ready, stream new rows and verify final key set and persistent TTLs.
- Live writes use existing 4096-row conversion slices and 128-row/1 MiB source
  pipelines, generated UUID defaults or explicit string keys, collision refusal,
  native/text encodings and exact values. Never replay unknown mutations/input.
- Empty input creates an empty collection or replaces all old rows with an empty
  collection retaining the supplied schema. Source data, unknown fields and TTLs
  are discarded only by explicitly admitted replacement. Unregister and managed
  GC never delete application rows. Each replacement changes a registration version to invalidate old live writers,
  without depending on query-projected columns.
- Replacement failures retain ADBC status/SQLSTATE and phase/outcome/acknowledged
  delete/write lower bounds. Readers may see partial state. No atomic rollback,
  source-client fencing, input/process resume or benchmark claim.
