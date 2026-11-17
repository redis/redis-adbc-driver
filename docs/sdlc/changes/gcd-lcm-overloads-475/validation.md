# Verification and review

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `19ebcb46`; working source described by [intent](intent.md), [spec](spec.md)
and [plan](plan.md). Local verification complete; PR review pending.
[Sanitized counts and source hashes](results.json).

## Reproduction and correction

Before the fix, the 408-case independent matrix matched **178 cases and failed
230**, with no infrastructure errors. Focused Go regressions reproduced ambiguous
unknown/smallint acceptance, typed invalid NULL acceptance, wrong literal errors,
and rejection of valid unknown literals beside typed numbers. Additional unit
regressions reproduced skipped type validation beneath IS NULL, casts and CASE.

A shared resolver now considers the integer, bigint and numeric signatures,
implicit integer widening and exact known-type matches. It rejects ambiguity
with 42725 and incompatible types with 42883, before strict NULL evaluation.
Unknown quoted literals take the selected type and use existing bounded numeric
parsers without a guessed fractional scale. Planning and execution use the same
selection; converted runtime arguments are private copies. The exact finite
coefficient arithmetic and 76-digit result boundary from #476 are preserved.

Bound expressions validate every GCD/LCM child, including wrappers and predicates
that need not infer child result types. Unbound DEFAULT checking performs the
same validation and converts internal SQLSTATE errors at the ADBC boundary.
An initial default experiment exposed a missing SQLSTATE conversion in the
candidate; final native cases verify 42725 and no published table. An initial
Go test queried DDL schema without executing it; it now executes CREATE through
ExecuteUpdate. Empty Arrow binding output may contain empty batches, so tests
drain the stream and reject any data rows instead of forbidding empty batches.

Integer parameter hints expose the selected exact Arrow width. Numeric-only
constraints do not specify a lossless parameter scale and retain an unknown
hint; actual bound Arrow types resolve the overload. Explicit casts retain
their declared parameter schema. No parameter field names or nullability rules
are changed.

## Commands and results

Owned Redis Software / Redis 8.8.0 and Search, three primary shards, noeviction,
authenticated trusted TLS/RESP3. `make -C go redis-up` initializes the fixture;
each live invocation re-enters software-setup's management gate for active and
effective QPF 4x, six Search workers, mnp routing and 32 connections. Client:
macOS arm64 through Docker published port. PostgreSQL 18.6 uses the pinned
amd64 reference image and C collation. Raw reports, credentials, trust material
and infrastructure identifiers stay private.

- `env -u REDIS_URI go test -count=1 -p 1 ./...`: all packages passed. Integration
  skips are expected in this unit-only run and do not prove Redis compatibility.
- `make -C go pgdiff-test`: **42 passed**.
- Focused `make -C go test-software` with `-race -run Test.*GCDLCM`:
  **nine top-level passes, zero failures/skips**, plus parameter/scalar subtests.
  Covers typed NULLs, unknown literals, integer widening, numeric scale, empty
  relations and bindings, wrappers/predicates/aggregate children, DEFAULT
  rejection without publication, parameter metadata, mixed-width/decimal bound
  arrays, reuse after refused bindings and unchanged caller values. Existing
  finite arithmetic, schema, update and overflow tests also pass.
- Final independent matrix: **425 matches, one explicit range exclusion, zero
  unexpected failures or infrastructure errors** (426 total). All 178 original
  passes remain; 229 original mismatches now match. Eighteen additional cases
  cover wrappers, DEFAULTs and integer overflow. The matrix retains every
  original SQL string/ID; no curated source/oracle/allowlist changed.
- The one exclusion is LCM(1, 1e-76): preserving input scale 76 requires a
  77-digit coefficient for the exact value 1, exceeding decimal256 precision 76.
  The existing 22003 bound is verified explicitly. This is not an equivalence
  pass or a new representation fix; #110/#362 retain broader numeric limits.
- Final full curated corpus: **605/645 dialect passes, 40 failures, five contexts,
  one extension, two existing allowed extensions, two exclusions and zero
  infrastructure errors** (655 total). No prior pass regressed. Exit 1 retains
  known compatibility failures, not infrastructure failures or a passing suite.
- Native build via the Makefile version linker flag, `git diff --check` and
  changed-document relative-target checks passed.

Reproduce the extra matrix from the repository root:

```sh
python3 go/pgdiff/gcd_lcm_overloads.py /private/test-output
cd go
bash software-setup.sh --exec make pgdiff PGDIFF_ARGS='--corpus /private/test-output --report build/gcd-lcm-overloads.json'
```

Use a disposable verified Software fixture. The generator is outside the
unchanged curated corpus and creates a separate comparison file. It requires no
external dataset and makes no performance claim.

## Review and remaining scope

Agent review followed REVIEW.md for type/error precedence, empty and lazy
execution, exact coefficients/scale, bounded parsing, input ownership, statement
reuse, no partial DEFAULT publication and unchanged storage/retry semantics.
No unresolved actionable finding remains within #475's three-signature scope.
Numeric nonfinite and broader representation work remain #362; the wider
function surface remains #110. No general PostgreSQL overload framework, full
Software Go suite, benchmark, human approval, hosted CI or merge is asserted.

## Main-branch conflict resolution

Merged `a9000d79` (PR #477) into this branch after it introduced MIN_SCALE in
the shared inference and scalar-dispatch functions. Both MIN_SCALE's validation
and execution branches and GCD/LCM's resolver/conversion branches are retained.
Reviewed the resulting diff against the new main; no MIN_SCALE behavior was
removed. Re-ran all Go unit packages with `env -u REDIS_URI go test -count=1 -p 1
./...` and the native Makefile build successfully. Integration skips are
expected in this unit-only rerun. The live comparison/race evidence above was
collected before this merge; it was not rerun for the conflict resolution.
