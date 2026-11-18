# Verification and review

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `5b0840ce`; implemented and locally verified, human PR review pending.
See [intent](intent.md), [spec](spec.md), [plan](plan.md) and
[sanitized counts/source hashes](results.json).

## Behavior and references

The parser maps `text IS [NOT] [NFC|NFD|NFKC|NFKD] NORMALIZED` to the public
IS_NORMALIZED function and an ordinary NOT expression. An omitted form defaults
to NFC. The one/two-argument ordinary function accepts text form expressions.
It shares NORMALIZE's signature checks, parameter metadata, CHAR coercion,
strict NULL handling, form errors and immutable/lazy planning behavior. It
returns Arrow boolean and compares the original text with the existing exact
Unicode 16 normalizer's output. No quick-check performance claim is made.
The normalization algorithm, Unicode tables and data licenses are unchanged.

Primary references:

- [PostgreSQL 18 string functions](https://www.postgresql.org/docs/18/functions-string.html).
- [PostgreSQL 18 grammar](https://github.com/postgres/postgres/blob/REL_18_STABLE/src/backend/parser/gram.y), IS NORMALIZED productions.
- [PostgreSQL 18 function catalog](https://github.com/postgres/postgres/blob/REL_18_STABLE/src/include/catalog/pg_proc.dat), is_normalized(text,text).
- [PostgreSQL 18 Unicode implementation](https://github.com/postgres/postgres/blob/REL_18_STABLE/src/backend/utils/adt/varlena.c).

## Checks

- Retained focused regressions fail before the implementation: valid predicate
  syntax was refused with 42601. Private before.log records the observations.
- Independent final matrix: **453/453 PostgreSQL 18.6 matches**, zero exclusions,
  unexpected failures or infrastructure errors. All forms/negations, ordinary
  calls, strict NULLs, type and argument-count errors, empty execution,
  precedence, literal/dynamic forms, lazy CASE/COALESCE, fixed CHAR, Unicode 16
  Tulu-Tigalari composition, all 35 Unicode 17 normalization-relevant additions
  as inert Unicode 16 starters, and long combining sequences are covered.
- `env -u REDIS_URI go test -count=1 -p 1 ./...`: all packages pass. Ordinary
  endpoint-dependent integration skips and the optional external normalization
  conformance-data skip are expected in this unit-only run. The unchanged full
  Unicode vector qualification remains historical evidence in PR #480; it is
  not relabeled as a rerun here.
- `make -C go pgdiff-test`: **42 pass**.
- Focused `make -C go test-software TEST_ARGS='-race -run Test.*IsNormalized'`:
  **four top-level tests pass, zero failures/skips**, plus seven binding
  subtests. Managed projection/filter/group/update, empty Arrow boolean schema,
  typed refusal, NULL/empty bindings, dynamic form errors, exact boolean values,
  full reader drain, unchanged caller input and statement reuse all pass.
- Native Makefile build via pgdiff preserves the version linker flag.
- Full unchanged curated corpus: **605/645 dialect passes, 40 known failures,
  five contexts, one extension, two existing allowed extensions, two exclusions
  and zero infrastructure errors** (655 total), retaining the baseline pass/failure counts.
  The harness exits 1 for retained known compatibility failures (make exits 2);
  this remains a failing compatibility suite, not full PostgreSQL conformance.

Live runs use an owned Redis Software / Redis 8.8.0 Search fixture with three
primary shards, authenticated trusted TLS/RESP3 and noeviction. Setup uses
`make -C go redis-up`; every live invocation re-enters the management gate for
active/effective 4x QPF, six Search workers, mnp routing and 32 connections. The
client is macOS arm64 through a Docker published port. Docker's predefined
network pools were exhausted, so an overlap-checked explicit subnet in a private
Compose override was used; unrelated networks were preserved. Raw reports,
credentials, trust files, override and infrastructure identifiers stay private.
The owned fixture/volumes/network and checkout-local test environment/trust files
were removed after verification. Changed-document relative links, source hashes
and `git diff --check` passed.

Reproduce from the repository root with a disposable verified Software fixture:

```sh
python3 go/pgdiff/is_normalized.py /private/normalized-corpus
cd go
bash software-setup.sh --exec make pgdiff PGDIFF_ARGS='--corpus /private/normalized-corpus --report build/is-normalized.json'
```

## Separately discovered DEFAULT grammar defect

The first matrix had 452 matches and one mismatch because a negative DEFAULT
signature query omitted the parentheses PostgreSQL requires around IS predicates.
The signature case now uses `DEFAULT (NULL::integer IS NORMALIZED)` and matches
42883 on both implementations. Independently probing the existing expression
`CREATE TABLE ... (b BOOLEAN DEFAULT 1 IS NULL)` reproduces a generic grammar
gap: PostgreSQL42601 versus successful native CREATE. Filed
[#481](https://github.com/redis/redis-adbc-driver/issues/481) with the existing bug
label at creation. The separate two-case probe has one matched parenthesized
signature and one unmatched unparenthesized existing predicate, with zero
infrastructure errors; it is not counted as a feature pass or hidden exclusion.
This is distinct from #479's deferred value evaluation work. The predicate
implementation does not broaden DEFAULT grammar scope.

## Review

Agent review followed REVIEW.md for precedence, NULL/NOT behavior, text/type
error ordering, empty and unreachable branches, exact Unicode bytes, Arrow
boolean result/schema, input and reader ownership and prepared reuse. Existing
normalization is reused without changing storage/publication or retry semantics.
No unresolved actionable finding remains in this bounded predicate/function
scope; the generic DEFAULT parser limitation remains explicitly #481. No full
Software Go suite, benchmark, human approval, hosted CI or merge is asserted.

## Main-branch conflict resolution

Merged `3e4aecfa` (PR #482) after FACTORIAL landed on main. The only conflicts
were the immutable-function whitelist and appended pgdiff documentation. Both
FACTORIAL and IS_NORMALIZED remain in the whitelist, both documentation sections
are retained, and the automatically merged registry/type/dispatch changes were
reviewed against main to confirm the FACTORIAL implementation is preserved.

Re-ran all Go unit packages with `env -u REDIS_URI go test -count=1 -p 1 ./...`
and the native Makefile build: both passed. Integration and optional external
conformance-data skips remain expected in the unit-only run. No live Software
fixture or differential comparison rerun was needed for the adjacent whitelist
and documentation merge; the matrix/race evidence and source hashes above
describe the pre-merge qualification. Changed-document targets and
`git diff --check` passed after resolution.
