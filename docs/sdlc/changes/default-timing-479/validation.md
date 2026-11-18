# Verification and review

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Original base: `5b0840ce`; merged main `8eb98ae0` (FACTORIAL #482 and
IS NORMALIZED #483). Candidate `0c685f94`; local verification complete,
PR review pending. See [intent](intent.md), [spec](spec.md), [plan](plan.md),
[sanitized counts/source hashes](results.json).

## Definition and use boundaries

CREATE TABLE, SET DEFAULT and retained-default validation during ALTER TYPE
now check names, forbidden forms, signatures, inferred result/assignment types
and type input without executing computed defaults. Typed NULLs retain their
logical UUID/JSON/array identity; array compatibility and explicit casts check
element types. Unknown string input is validated at definition time, including
ARRAY literal elements and parser-internal JSON and single-field INTERVAL forms.
Assignment numeric/string typmods are enforced when values are used. Explicit
cast errors retain cannot_coerce 42846 rather than being rewritten as outer
assignment errors. Direct NULL constants may be omitted from metadata;
nontrivial typed NULL conversion expressions retain their SQL/type identity.
Local-time literal freezing continues through the existing stored-SQL path.

ADD COLUMN separately evaluates/materializes its nonvolatile default, including
on an empty table. Its existing volatile-ADD restriction stays in place.
SQL INSERT/INSERT SELECT and MERGE plan referenced constant defaults even with
no input rows, without planning a default replaced by an explicit value/NULL.
The existing folder's explicit pure-function whitelist inspects immutable
constants; stable/volatile values stay deferred to execution. SQL/Arrow fill,
statement caching, value coercion, row constraints and private publication
continue using the existing paths. No storage format or retry protocol changes.

The CASE folder now requires a known selected THEN/ELSE value before evaluating
the CASE during planning. Merely knowing a condition must not execute or cache a
volatile result. SET_CONFIG is registered volatile so defaults call it per row.
These shared fixes also prevent an extra planning-time RANDOM draw; the parallel
random-function work retains the same CASE correction as a separate commit.

## Independent evidence and checks

- Before logs reproduce eager division/UNISTR/typmod errors, missing assignment
  identity and skipped invalid typed literal input. A separate focused test
  reproduced selected volatile CASE execution and caching during planning.
- **52/52 independent PostgreSQL lifecycle scenarios pass**, covering **170 SQL
  steps**, with **zero exclusions, unexpected failures or infrastructure
  errors**, after merging #482/#483. Each step pins PostgreSQL's expected error
  SQLSTATE or success, then compares the native driver outcome. Tests check
  state after failures as well as successful CREATE/ALTER/INSERT responses.
- Includes arithmetic/Unicode/cast errors, numeric/string typmods, direct
  versus typed text input, UUID identity, recursive array assignments/casts,
  JSON/JSONB conversion, interval literal input, typed NULL metadata and ALTER
  TYPE rejection, lazy CASE/COALESCE, explicit NULL/value bypasses, empty SQL
  sources, MERGE, volatile session effects and non-consumption of sequence
  counters during definition/constant planning.
- **20 focused top-level Software race tests passed, zero failures/skips**:
  new timing/type/metadata/CASE/binding/effect cases plus existing default
  insert/validation/MERGE/ingest/ADD/SET/temporary/concurrent-snapshot,
  creation-zone, length, CASE and SQL sequence tests. Native bindings verify
  explicit fields/NULLs bypass defaults, omitted SQL/Arrow fields fail without
  publishing rows, input arrays stay unchanged and statements work after a
  refused default and subsequent ALTER.
- `env -u REDIS_URI go test -count=1 -p 1 ./...`: all Go packages passed after
  the main merge. Endpoint-dependent integrations and optional external Unicode
  conformance data skip as expected in this unit-only run.
- `make -C go pgdiff-test`: **42 passed**.
- Native Makefile build with the version linker flag passed through pgdiff.
- Full unchanged curated corpus after the merge: **605/645 dialect passes,
  40 known failures, five contexts, one extension, two allowed extensions,
  two exclusions, zero infrastructure errors** (655 total). Comparison exits
  nonzero for retained compatibility failures; this is not a passing full
  compatibility suite. No SQL/oracle/allowlist or comparison policies changed.

Live runs used owned Redis Software / Redis 8.8.0 and Search, three primary
shards, authenticated trusted TLS/RESP3 and noeviction. `make -C go redis-up`
initialized the fixture; every live invocation used software-setup's management
gate for active/effective QPF 4x, six Search workers, mnp routing and 32
connections. Client: macOS arm64 through a Docker published port. Reference:
pinned PostgreSQL 18.6, C collation and UTF-8. Raw logs, credentials, trust
material and operational identifiers remain private. No full Software Go suite
or benchmark is claimed. The owned fixture/volumes and checkout-local trust/
environment files were removed after validation. Document link/JSON/source-hash
checks and `git diff --check` passed.

Reproduce the independent corpus from the repository root:

```sh
python3 go/pgdiff/default_timing.py /private/default-corpus
cd go
bash software-setup.sh --exec make pgdiff PGDIFF_ARGS='--corpus /private/default-corpus --report build/default-comparison.json'
```

Focused race filter:

```text
Test(SQLDefault|DefaultDefinition|DefaultPlan|DefaultTyped|CreationZoneDefault|SQLLengthDefaults|SQLSequencesIdentityAndDMLCTE|SQLSequenceConcurrentConnections|SQLAddColumnDefault|SQLAlterColumnDefault|SQLCase)
```

Use a disposable fixture and verify effective Software 4x QPF before testing.

## Review and remaining scope

An independent subagent reviewed the candidate read-only and used separate
PostgreSQL probes. Findings corrected before delivery: volatile CASE purity,
UUID/array assignment identity, explicit-cast error state and array input,
nontrivial typed NULL metadata, JSON cast inputs/conversions and single-field
interval literals. Final review reported no remaining actionable regression
within #479's definition/use timing scope. The author reviewed correctness,
security/ownership and intent/evidence per REVIEW.md. This is agent review,
not human approval.

Historical tests asserting eager precision/length errors were updated to verify
that definitions succeed, use fails and no rows publish. Matching typed NULL
metadata behavior and creation-zone behavior are retained. Tests were not
weakened to accept new mismatches.

Separate pre-existing defects stay open: [#481](https://github.com/redis/redis-adbc-driver/issues/481)
DEFAULT grammar, and [#484](https://github.com/redis/redis-adbc-driver/issues/484)
retained unknown-literal typing across ALTER TYPE. For #484, both the baseline
and candidate reject changing NUMERIC DEFAULT '1.50' to INTEGER; PostgreSQL
accepts it and later inserts 2. That historical typing-identity problem is not
reported as fixed or counted as a passing lifecycle case here. UPDATE SET
DEFAULT and volatile ADD COLUMN remain existing unsupported forms. Broader
PostgreSQL/SQL conformance, human approval, hosted CI and merge are not asserted.
