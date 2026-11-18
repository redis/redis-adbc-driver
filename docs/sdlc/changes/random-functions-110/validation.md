# Verification and review

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `5b0840ce`. Local feature verification complete; #479 integration and
PR review pending. See [intent](intent.md), [spec](spec.md), [plan](plan.md)
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
- Full matrix with the retained selected-volatile CASE regression:
  **214/216 matches, two known #479 failures**, zero infrastructure errors.
  The seed setup passes; CASE and its continuation expose an extra planning
  draw. These failures remain visible in the generated matrix, with no
  allowlist, context policy, skip or unsupported classification.
- Focused Software race run: **seven top-level tests pass**, zero failures or
  skips, plus bound-array subtests. Covers independent PostgreSQL seed/stream
  vectors, session isolation, NULL/empty/lazy calls, failed seed preservation,
  a failed managed CHECK consuming RANDOM exactly once without publishing a
  row, rollback preserving state, Arrow void validity, bound means/deviations,
  NULL/empty/invalid arrays, parameter metadata, caller ownership and reuse
  after refused bindings. A concurrent session-state test uses eight workers.
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

Use a disposable verified Software fixture. Until #479 lands, the command
retains two CASE failures as described above rather than exiting successfully.

## Required integration dependency and review

[#479](https://github.com/redis/redis-adbc-driver/issues/479) is required before
claiming complete DEFAULT/selected-CASE evaluation timing. Its existing generic
DEFAULT path evaluates expressions at definition time; its existing CASE folder
also evaluates a selected volatile result during planning. With seed .5,
PostgreSQL's selected CASE returns the first uniform draw, while this baseline
returns the second and shifts the next draw too. Existing RANDOM() reproduces
this independently of the new functions. The root change fixes that planning
boundary; this branch does not duplicate defaults.go or case_planning.go changes.
The three-statement probe is retained in the default generated matrix.

Agent review followed REVIEW.md for signed seed conversion, strict NULL and
literal precedence, partial/failed statement effects, Arrow validity/ownership,
stream lifetimes and consumption, session locking, pseudo-type persistence and
unchanged retry semantics. No other reproduced actionable finding remains in
this bounded function scope. PostgreSQL range-taking RANDOM overloads remain
#110. No full Software suite, benchmark, human approval, hosted CI or merge is
asserted.
