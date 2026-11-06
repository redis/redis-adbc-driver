# Schema-qualified snapshot rows and physical-prefix ownership

Date: 2026-11-03. Parent source `9b80edc`. Local isolation checkpoint;
[full parity](plan.md) remains incomplete. **Breaking storage protocol 13**
rejects earlier drafts 1–12 without modification. There is no migration.

## Implementation

Protocol 12 placed managed row HASHes inside the `adbc:*` metadata grant. A
real Software API principal reproduced direct cross-schema row access under
the old public-schema recipe. Protocol 13 uses
`<escaped-schema>:{meta}:snapshot:rows:<uuid>:`. Escaping prevents schema names
from injecting another hash tag or ACL glob. Schema `adbc` uses `%61dbc` so its
rows also remain outside the metadata grant. Metadata and exact row directories
retain their existing namespace. Every physical key still uses `{meta}`;
this does not distribute a catalog's rows across shards.

Data ownership records retain the exact physical prefix. Private allocation
registers it atomically with ownership/references. Descriptor registration and
row mutation fences reject another schema prefix with the same UUID. Search
admission additionally requires `FT.CREATE` to declare exactly its owned prefix;
rejected admission cannot create a distributed index. Per-command admission
reuses validated owner/prefix records for duplicate directory/data UUIDs.
Distributed Search operations remain top-level commands with pre/post admission.

Durable compaction jobs carry and validate their output prefix against the source
schema. Copy/index/install preserve that namespace. Zero-reference reclamation
uses the graph's registered prefix after schema removal, rather than reconstructing
a default public prefix. Existing reference, pin, lease, checkpoint and atomic
publication rules remain. No SQL, Arrow or uncertain Search operation is replayed.

## Verification

- `make -C go redis-up`: owned Software fixture, active effective 4x QPF,
  three primary shards, noeviction, strict TLS.
- Canonical-prefix unit checks cover public/private/temp schemas, schema `adbc`,
  Unicode and escaped `:{...}*` names, exact row IDs above 2^53, fixed hash tag,
  and rejection of legacy/noncanonical/invalid UUID layouts.
- Focused race tests pass for three schema namespaces: compaction, DROP while an
  old reader remains pinned, old-value retention, eventual row reclamation and
  preservation of unrelated public data. Alias tests reject row writes,
  descriptor registration and Search creation under another physical prefix.
- Final full snapshot race pass: 133 pass events, seven expected Cluster-only
  skips, zero failures, 136.449 seconds. Earlier complete passes took 132.128
  and 132.616 seconds; none are performance measurements.
- Final unit-only `go test -json -count=1 -p 1 ./...`: 898 pass events,
  712 expected integration skips, zero failures. Skips are not compatibility
  evidence.
- Wide HASH/Arrow regular/partition race test: passed, 71.701 seconds.
  Managed-import report race cases: two passed, 3.553 seconds. CLI adoption race
  case: passed, 2.717 seconds.
- Rebuilt final native library: snapshot validation 12 passed (14.354 seconds),
  IPC nine passed (1.11 seconds), Ray 17 passed (13.25 seconds, three Ray
  configuration deprecation warnings). No interface cases skipped.
- Final rebuilt-library Software API isolation gate: passed again after all
  native checks. `gofmt`, Python formatting/help, relative links and
  `git diff --check`: passed.

The [Software API isolation gate](../../../../scripts/snapshot_acl_scope.py)
now verifies metadata, public HASH and public Search access as positive controls.
It requires `HGETALL`, `FT.SEARCH` and `FT.AGGREGATE` to deny both an ordinary
private schema and schema `adbc`. Result on the repaired layout:

```json
{"software":true,"qpf":4,"noeviction":true,"strict_tls":true,"schema_isolated":true,"protected_schemas_checked":2,"row_read_commands_checked":3,"owned_resources_removed":true}
```

All six negative checks and the positive controls pass. Owned SQL objects,
API users, roles, ACL rules and mappings were removed; temporary credentials were
deleted and no infrastructure identifiers were published. The initial expanded
probe assumed RESP2 Search arrays and failed its positive-control parser on RESP3
maps. Its cleanup completed; the corrected probe requires RESP3 and validates
`total_results`. No authorization assertion was weakened.

## Scope and author review

Reviewed canonical key parsing, metadata namespace separation, fixed hash tags,
server-side prefix registration/validation, partial-failure ordering, durable
job/source identity, reclamation after schema removal, old reader retention and
probe cleanup. Self-review is not maintainer approval.

This fixes the reproduced HASH/Search data-access boundary. It does not establish
complete restricted ADBC reader/writer command and pin recipes, raw metadata
write isolation, SQL privilege management, hosted/platform/Cluster API recovery
or performance parity. Current data remain in one catalog slot. Imported `__key`
identity, remaining adoption/sweep/ACL families, large atomic operations, sustained
compaction and broader benchmarking remain required work. Historical protocol-12
measurements retain their original labels.
