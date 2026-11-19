# Validation

Validation is recorded after implementation; operational fixture identifiers,
credentials, certificates and raw private infrastructure logs are excluded.

* Pure Go parser/metadata tests cover ordered declarations, FK arity, generated
  names, duplicate constraint names, immutable clones, unresolved targets,
  renamed columns and replacement target identities.
* Software lifecycle test covers persisted UNIQUE/FK metadata, ordered FK pairs,
  duplicate/orphan acceptance, target table/column rename, managed prefix rekey,
  type rewrite, TRUNCATE, drop/name reuse and ALTER add/drop dependencies.
* Existing CHECK/PRIMARY KEY regressions verify unchanged enforcement scope.
* Full unit-only package suite and native ADBC validation results are recorded
  by the consolidating change; skipped Redis tests do not prove compatibility.

Completed locally:

* `env -u REDIS_URI go test -count=1 -p 1 ./...` — PASS (unit-only;
  integration skips expected).
* `go test -count=1 -p 1 -timeout 12m -run
  '^TestSQL(Declaration|Constraint|Check|PrimaryKey)' .` — PASS through the
  verified Redis Software setup wrapper: Redis 8.8.0, three shards, active
  verified 4x QPF. The two preexisting assertions that intentionally omitted
  formerly discarded declarations were updated to the new metadata contract.
* Native feature flags are enabled for UNIQUE/FK using the existing fixtures;
  the consolidating change runs the exact upstream native ADBC tests.
