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

Follow-up review found and corrected two lifecycle gaps before consolidation:

* Managed snapshot ALTER now prefetches FK targets before its pure planner, as
  the ordinary ALTER path does. A Software regression checks both ADD CONSTRAINT
  and ADD COLUMN REFERENCES against an existing parent.
* Fresh descriptors have a declaration epoch, preventing a same-name Arrow
  replacement column from receiving an old FK reference when the public OID is
  preserved. Epoch assignment happens only in fresh constructors, not generic
  create/clone/rewrite helpers; legacy descriptors keep their old identity.
  Regression verifies Arrow append retains usage and same-name replace clears it.
* The full selected Software declaration/CHECK/PRIMARY KEY suite was rerun and
  passed after both fixes, with setup-verified active 4x QPF.

The consolidating full Software suite exposed an existing cleanup-test scheduling
assumption after an ACL fixture left unrelated retired data. A reaper step may
retire another manifest and enqueue its data before retrying the denied group;
its equal-score data queue orders candidates by ID. The test helper now allows
at most 256 successful successor steps within the same 10-second context, checks
the pinned live source after every step, fails any error, and requires the target
three rows plus its index/directory/graph registration to be removed. An explicit
10-second < 2-minute snapshot lease assertion preserves the no-lease-wait check.
No production cleanup behavior changes. The isolated original pair passed ten runs (the original failure needed broader
queue state); the patched pair also passed ten runs. Final Go unit packages,
25 focused Software tests and the native 581-test suite passed. See the
[combined evidence](../adbc-validation-gaps/validation.md) and
[PR #543](https://github.com/redis/redis-adbc-driver/pull/543).
