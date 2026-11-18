# Verification and review

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `5b0840ce`, with main `3e4aecfa` (FACTORIAL) merged. Local verification
complete; PR review pending. See [intent](intent.md), [spec](spec.md), [plan](plan.md)
and [sanitized results/source hashes](results.json).

## Design and provenance

Each connection owns one synchronized random state. SETSEED, RANDOM() and
RANDOM_NORMAL share it; UUID generation remains on its separate entropy path.
The xoroshiro128** state transition, splitmix64 seed expansion, signed 52-bit
float seed mapping and 52-bit uniform extraction match PostgreSQL 18.
RANDOM_NORMAL uses its two-uniform Box-Muller transform. Unseeded sessions use
crypto entropy to initialize their state, but generated SQL samples are not
cryptographic. A lock spans each complete draw or seed operation, providing
race-free state transitions without promising evaluation order for concurrent
statements. No snapshot publication, retry or replay protocol was changed.

SETSEED returns logical void with an empty, non-NULL Arrow string; NULL seed
returns NULL without touching state. PG_TYPEOF retains void. Table/view
persistence is rejected with 42P16, disallowed casts with 42846 and implicit
text overloads/operators with 42883. Explicit text casts, concatenation and
COALESCE retain their tested PostgreSQL behavior. Derived-column checks retain
that logical identity. This is a physical Arrow mapping, not a new persisted
database type or a general user-defined type framework.

SETSEED accepts float8-compatible numeric types and unknown literals, rejecting
typed text/boolean inputs even when NULL or empty. Invalid seeds give 22023;
failed seeds do not replace state. RANDOM_NORMAL supports zero, one or two
arguments (mean=0, stddev=1), including mean/stddev named notation. Negative
stddev and IEEE special-value arithmetic follow PostgreSQL rather than adding
an extra validation rule. Strict NULLs draw nothing. Unknown string-to-float
conversion errors are resolved before row access (22P02). Each function is
registered volatile and exposes float64 parameter hints.

Primary sources:

- [PostgreSQL18 mathematical functions](https://www.postgresql.org/docs/18/functions-math.html).
- [PostgreSQL18 SQL pseudorandom functions](https://github.com/postgres/postgres/blob/REL_18_STABLE/src/backend/utils/adt/pseudorandomfuncs.c).
- [PostgreSQL18 PRNG implementation](https://github.com/postgres/postgres/blob/REL_18_STABLE/src/common/pg_prng.c).
- [PostgreSQL source license](../../../../go/random-PostgreSQL-LICENSE.txt).

Uniform outputs use exact binary comparison. Normal results can differ by a
few ULP between Go math and PostgreSQL's platform C library: independent scalar
oracle vectors assert a documented `2e-14 * max(1, abs(expected))` tolerance;
SQL comparisons explicitly round normal results to nine fractional digits.
No global comparator tolerance or expected-result rewrite was added.

## Checks and actual results

Owned Redis Software / Redis8.8.0 with Search, three primary shards, noeviction,
trusted authenticated TLS/RESP3. `make -C go redis-up` initialized the fixture;
every live invocation re-entered the management gate for active/effective QPF
4x, six Search workers, mnp routing and 32 connections. Client: macOS arm64
through Docker's published port. Reference: pinned PostgreSQL18.6, C collation.
Raw logs, credentials, trust material and infrastructure identifiers stay private.

- Independent feature matrix: **213/213 matches**, zero exclusions or
  infrastructure errors. Includes six seed values, exact uniform sequences,
  interleaved normal draws, reseeding, named argument errors, types/typed NULLs,
  unknown literals, seed ranges/NaN/infinity, empty relations, Arrow-equivalent
  result types, isolated sessions and logical void behavior.
- Final full matrix, including the retained selected-volatile CASE regression:
  **216/216 matches**, zero exclusions or infrastructure errors. Before the
  shared CASE purity correction, 214/216 matched: the selected CASE consumed
  an extra planning draw and shifted its continuation. The two previously
  failing SQL statements remain ordinary passing cases after the fix; no
  allowlist, context policy, skip or unsupported classification was added.
- Focused Software race run: **nine top-level tests pass**, zero failures or
  skips, plus bound-array subtests. Covers independent PostgreSQL seed/stream
  vectors, session isolation, NULL/empty/lazy calls, failed seed preservation,
  a failed managed CHECK consuming RANDOM exactly once without publishing a
  row, rollback preserving state, Arrow void validity, bound means/deviations,
  NULL/empty/invalid arrays, parameter metadata, caller ownership and reuse
  after refused bindings. Selected THEN/ELSE and nested CASE, normal draws,
  seed operations and schema inspection are tested for zero planning-time
  consumption. A concurrent session-state test uses eight workers.
- `env -u REDIS_URI go test -count=1 -p 1 ./...`: all packages pass. Endpoint
  integration and the optional external Unicode conformance test skip in this
  unit-only run. The latter is unrelated to this random-function change.
- `make -C go pgdiff-test`: **42 pass**.
- Native Makefile build with the version linker flag passes.
- Full curated corpus: **605/645 dialect passes, 40 known failures, five
  contexts, one extension, two existing allowed extensions, two exclusions and
  zero infrastructure errors** (655 total). It remains a failing compatibility
  suite. No curated source SQL, oracle expectations, policies or allowlist changed.

An intermediate expanded matrix caught a mixed positional/named error that
needed 42883 rather than the repeated-named 42601. It now matches PostgreSQL.
An initial unit helper assumed all malformed arities parsed; it now inspects
parse-time SQLSTATEs too. The legacy RANDOM(1) message remains unchanged, since
range-taking RANDOM overloads are outside this change. Initial native test
corrections used the harness schema helper and properly quoted the run regex.

Reproduce the separate matrix from the repository root:

```sh
python3 go/pgdiff/random_functions.py /private/random-corpus
cd go
bash software-setup.sh --exec make pgdiff PGDIFF_ARGS='--corpus /private/random-corpus --report build/random-functions.json'
```

Use a disposable verified Software fixture.

## Shared CASE correction and remaining DEFAULT issue

The generic CASE folder previously treated a selected volatile result as a
known value even when its child folder said it was unknown. The exact two
known-propagation corrections coordinated with #479 are included as a separate
commit: selected THEN and ELSE values must be known before evaluating the
whole CASE. No function whitelist change was copied, and the merged FACTORIAL
whitelist is preserved. Native seeded tests now prove zero planning-time draws
for selected/nested CASE and schema inspection, and exactly one execution-time
draw. The three-statement independent regression remains in the generated
matrix and passes.

[#479](https://github.com/redis/redis-adbc-driver/issues/479) still owns generic
DEFAULT definition-time evaluation. The baseline evaluates volatile defaults
when defining them; the separate #479 change removes that
behavior. This PR does not duplicate defaults.go changes or claim that issue
closed. CREATE/SET DEFAULT lifecycle qualification should include that fix.

After main's FACTORIAL PR #482 landed, shared registry, type-validation and
documentation conflicts were resolved preserving both features. All Go unit
packages and the native build passed again after integration; focused race
and 216-case comparison runs also passed with the shared CASE fix.

Agent review followed REVIEW.md for signed seed conversion, strict NULL and
literal precedence, partial/failed statement effects, Arrow validity/ownership,
stream lifetimes and consumption, session locking, pseudo-type persistence and
unchanged retry semantics. No unresolved reproduced actionable finding remains in
this bounded function scope beyond the separately documented generic DEFAULT issue. PostgreSQL range-taking RANDOM overloads remain
#110. No full Software suite, benchmark, human approval, hosted CI or merge is
asserted.

The owned fixture, volumes, explicit project network and checkout-local test
environment/trust files were removed after the final runs. Changed-document
relative targets, sanitized JSON/source hashes and `git diff --check` pass.

Main's normalization predicate PR #483 (`8eb98ae0`) was merged afterward.
Its README section was retained, and code merged without conflicts; the shared
CASE correction and FACTORIAL/predicate pure-function registry entries remain
intact. All Go unit packages and the native build were checked again after
that merge. The live 216/race/full-corpus evidence above precedes this final
main merge; no additional live rerun is claimed for it.
