# Verification and review

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `5b0840ce`. Local verification complete; PR review pending.
See [intent](intent.md), [spec](spec.md), [plan](plan.md) and
[sanitized results/source hashes](results.json).

## Contract and implementation

PostgreSQL 18 exposes `factorial(bigint) -> numeric`; smallint/integer inputs
widen to bigint and unknown quoted literals use bigint input syntax. Typed
numeric/float/text/boolean/array values have no implicit signature, including
NULLs and expressions evaluated over empty input. The shared input check is
used by inference, runtime and DEFAULT validation; wrapper traversal validates
calls hidden under casts, IS NULL and unused CASE branches. Argument counts
carry 42883. Parameter hints use Arrow int64.

Runtime arithmetic uses exact big integers and returns decimal256(76,0).
Zero and one return one, negatives return 22003, and NULL returns typed numeric
NULL. Each result owns its coefficient. Values above 56 are refused before any
unbounded arithmetic: 56! has 75 digits; 57! has 77. PostgreSQL can represent
factorials through 32177, so **57..32177 remain a compatibility gap**, not a
passing implementation of PostgreSQL's complete result domain. No numeric
representation/storage protocol, cast semantics or Arrow type is widened here.
The existing immutable constant planner inspects FACTORIAL while preserving
lazy CASE/COALESCE evaluation and without mutating the AST.

Primary references:

- [PostgreSQL 18 math functions](https://www.postgresql.org/docs/18/functions-math.html).
- [PostgreSQL 18 numeric_fac implementation](https://github.com/postgres/postgres/blob/REL_18_STABLE/src/backend/utils/adt/numeric.c), including negative-input and 32177-bound checks.

Live probes verified the signature, 56! and 57!'s digit count, bigint input
syntax, unknown/typed NULLs, negative and huge inputs, constant errors over
empty results, and lazy value branches versus eager unknown-literal conversion.

## Evidence

- Focused tests fail before implementation with missing-function errors for
  supported scalar/signature/value/empty cases; private before.log retains the
  failures. One initial expected 56! string had three extra zeroes; corrected
  from the independent PostgreSQL result before final tests.
- Independent matrix: **126 exact PostgreSQL matches, four explicit Arrow-range
  exclusions, zero unexpected failures or infrastructure errors (130 total)**.
  All results 0!..56! compare exactly. Remaining cases cover integer widths,
  unknown literal bases/separators/whitespace, typed NULLs, incompatible types,
  malformed/overflowing input, negative and huge arguments, signatures/counts,
  wrappers, qualified calls, nested calls, lazy branches and empty queries.
  The four exclusions are 57!/100! with and without WHERE FALSE; PostgreSQL
  succeeds while the candidate returns the explicitly verified 22003 boundary.
  They are not equivalence passes, nor added to the curated oracle/allowlist.
- Focused `make -C go test-software TEST_ARGS='-race -run Test.*Factorial'`:
  **five top-level passes, zero failures or skips**, plus prepared-binding
  subtests. Managed SELECT/filter/UPDATE, valid and invalid DEFAULT signatures,
  failed UPDATE without partial publication, Arrow decimal256 schema/values,
  int16/int32/int64 widening, NULL and empty bindings, invalid types/value errors,
  statement reuse, complete fetch, unchanged input and int64 parameter metadata
  are verified.
- `env -u REDIS_URI go test -count=1 -p 1 ./...`: all packages passed.
  Endpoint-dependent integrations and the optional external Unicode conformance
  file test skip in this unit-only run; they are not Software coverage evidence.
- `make -C go pgdiff-test`: **42 passed**. Native Makefile build with the
  driver's version linker flag passed.
- Full unchanged curated corpus: **605/645 dialect passes, 40 known failures,
  five contexts, one extension, two existing allowed extensions, two exclusions,
  zero infrastructure errors (655 total)**. The comparison retains exit 1 for
  known failures; no full PostgreSQL compatibility claim follows.

Live validation used an owned Redis Software / Redis 8.8.0 Search fixture,
three primary shards, authenticated trusted TLS/RESP3, noeviction and verified
active/effective QPF 4x (six Search workers, mnp routing, 32 connections).
`make -C go redis-up` initialized it; every live run re-entered software-setup's
management gate. Client: macOS arm64 through Docker published port. Reference:
pinned PostgreSQL 18.6 linux/amd64, C collation. Raw logs, credentials, trust
material, networking and other operational identifiers remain private. Initial
Docker default address-pool exhaustion was resolved with an isolated owned
network; no unrelated fixture/network was pruned or changed. The owned
fixture, volumes, network and checkout-local environment/trust files were removed
after verification. Changed Markdown targets, JSON/source hashes and
`git diff --check` passed.

Reproduce from the repository root on an owned verified Software fixture:

```sh
python3 go/pgdiff/factorial.py /private/factorial-corpus
cd go
bash software-setup.sh --exec make pgdiff PGDIFF_ARGS='--corpus /private/factorial-corpus --report build/factorial-comparison.json'
```

## Review and remaining scope

Agent review followed REVIEW.md for type/error precedence, strict NULL and empty
planning, lazy branches, bounded work, exact numeric/Arrow values, owned buffers,
prepared reuse and atomic publication after row errors. No actionable finding
remains within the bounded signature/result contract. Broader result-domain
representation remains explicit under #110; generic DEFAULT value-error timing
remains #479. No full Software Go suite, benchmark, hosted CI, human approval or
merge is asserted. Curated SQL, expectations, policies and allowlist are unchanged.
