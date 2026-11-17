# Verification and review

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `42f6e0b1`; local verification complete, PR review pending.
See [intent](intent.md), [spec](spec.md), [plan](plan.md) and
[sanitized counts/source hashes](results.json).

## Implementation and independent references

The unqualified NORMALIZE special form parses its optional NFC/NFD/NFKC/NFKD
keyword; qualified or quoted ordinary calls take one or two text expressions.
UNISTR takes one text argument. Registry resolution, type inference, bound
wrappers and unbound DEFAULT signature checks reject incompatible arguments
before NULL/empty execution. All parameter hints are strings. Runtime coercion
strips fixed CHAR padding, returns independent strings and preserves NULLs.
The existing immutable CASE planner now also inspects Unicode constant calls,
without rewriting the AST or evaluating provably unused CASE/COALESCE values.

UNISTR decodes once, consumes fixed-size hex escapes, joins only adjacent
surrogate escapes and never rescans emitted backslashes. Errors preserve 42601
for malformed syntax/pairs, 22023 for invalid scalar values/forms, 42883 for
signatures/counts and 22021 for invalid UTF-8/NUL text input.

PostgreSQL 18 uses Unicode 16.0; the current Go 1.27 x/text tables use Unicode
17.0. Direct x/text normalization also inserts CGJ in long nonstarter runs.
The implementation decomposes individual scalars, stably orders nonstarter
runs and composes unblocked pairs, preserving existing CGJs without inserting
new ones. Stable sorting avoids quadratic insertion-sort behavior. The 35
Unicode 17 additions with a decomposition or nonzero combining class are inert
starters under Unicode 16. Tests pin the x/text table version so dependency or
toolchain changes require requalification.

Primary references:

- [PostgreSQL 18 string functions](https://www.postgresql.org/docs/18/functions-string.html).
- [PostgreSQL 18 parser](https://github.com/postgres/postgres/blob/REL_18_STABLE/src/backend/parser/gram.y), NORMALIZE grammar.
- [PostgreSQL 18 string implementation](https://github.com/postgres/postgres/blob/REL_18_STABLE/src/backend/utils/adt/varlena.c), Unicode forms and UNISTR.
- [PostgreSQL 18 Unicode version](https://github.com/postgres/postgres/blob/REL_18_STABLE/src/include/common/unicode_version.h).
- [Unicode normalization](https://www.unicode.org/reports/tr15/) and the
  [Unicode 16 conformance data](https://www.unicode.org/Public/16.0.0/ucd/NormalizationTest.txt).
- [Unicode 17 ages](https://www.unicode.org/Public/17.0.0/ucd/DerivedAge.txt) and
  [Unicode 17 character data](https://www.unicode.org/Public/17.0.0/ucd/UnicodeData.txt).
  To derive the exclusions, select Age=17.0 code points whose UnicodeData
  combining-class field is nonzero or decomposition field is nonempty. There
  are exactly 35. This is a Unicode-version compatibility boundary, not an
  exception to the comparison oracle. [Unicode data license](../../../../go/unicode-LICENSE.txt).

Exact source SHA-256 values are in results.json. Conformance data remain outside
the repository; tests do not download them. The native library already depended
on x/text v0.44.0 indirectly; it is now direct, without a version upgrade.

## Verification

- Retained scalar/parser/empty regressions fail before registration and
  implementation, including missing functions, special-form parsing and NULL
  type resolution. Private before.log preserves the observations.
- Final independent matrix: **404/404 matches, zero exclusions or infrastructure
  errors** against pinned PostgreSQL 18.6, C collation and UTF-8. Covers all four
  forms, Unicode 16/17 boundaries, Hangul, composition exclusions, ordering,
  long combining runs, escaped backslashes, all escape widths, surrogate
  boundaries, invalid digits/truncation/zero/range, strict NULLs, typed arguments,
  CHAR coercion, wrong argument counts, empty input, lazy CASE/COALESCE and
  invalid DEFAULT signatures. An expanded intermediate run exposed four
  missing argument-count SQLSTATEs; all four now match 42883.
- Full Unicode 16 normalization conformance: **19,965 vectors and 399,300 exact
  assertions passed** (five inputs by four forms per vector). Separately tests
  all 35 Unicode 17 normalization-relevant additions as inert Unicode 16
  starters, plus existing CGJ and long nonstarter preservation.
- `env -u REDIS_URI go test -count=1 -p 1 ./...` with the conformance-file
  environment variable: all packages passed. Ordinary endpoint-dependent
  integration tests skip in this unit-only run; the optional conformance test
  runs when its file is supplied and otherwise explicitly skips.
- `make -C go pgdiff-test`: **42 passed**.
- Focused `make -C go test-software TEST_ARGS='-race -run Test.*Unicode'`
  with the conformance file supplied: **nine top-level passes, zero failures or
  skips**, including existing SQL/JSON Unicode coverage, managed rows, grouping,
  filtering, updates, empty relations, valid/lazy defaults, Arrow string result
  types, parameter metadata, empty and NULL bindings, invalid bindings,
  complete fetch, input ownership and statement reuse after failures.
- Full unchanged curated corpus: **605/645 dialect passes, 40 known failures,
  five contexts, one extension, two existing allowed extensions, two exclusions,
  zero infrastructure errors** (655 total). Exit 1 preserves the known
  compatibility failures; this is not a fully passing compatibility suite.
- Native Makefile build with the version linker flag passed. No full Software
  Go suite or benchmark is claimed. No curated SQL, expectations, comparison
  policies or allowlist were changed.

Live runs used an owned Redis Software / Redis 8.8.0 Search fixture, three
primary shards, authenticated trusted TLS/RESP3 and noeviction. Setup ran
`make -C go redis-up`; every live invocation re-entered the management gate for
active/effective QPF 4x, six Search workers, mnp routing and 32 connections.
The client was macOS arm64 through a Docker published port. Credentials, raw
reports, trust material and infrastructure identifiers remain private. The owned
fixture/volumes and checkout-local test environment/trust files were removed
after verification. Changed-document relative links, JSON/source hashes and
`git diff --check` passed.

Reproduce from the repository root, using a disposable verified Software fixture:

```sh
python3 go/pgdiff/unicode_functions.py /private/unicode-corpus
cd go
bash software-setup.sh --exec make pgdiff PGDIFF_ARGS='--corpus /private/unicode-corpus --report build/unicode-comparison.json'
curl -fsSL https://www.unicode.org/Public/16.0.0/ucd/NormalizationTest.txt -o /private/NormalizationTest16.txt
UNICODE_NORMALIZATION_TEST_FILE=/private/NormalizationTest16.txt go test -count=1 -p 1 -run TestUnicodeNormalizationConformance -v .
```

Verify the downloaded file's SHA-256 against results.json before using it as
qualification evidence. Default fixture management, ports and paths above are
examples, not retained infrastructure or credentials.

## Separate discovered defect and review

Independent probes show PostgreSQL accepts both `DEFAULT 1/0` and
`DEFAULT unistr('\xyz')` definitions; this driver's generic checkDefault evaluates
them during CREATE and fails early. The arithmetic reproduction predates this
feature. Filed [#479](https://github.com/redis/redis-adbc-driver/issues/479) with the
existing bug label at creation. The Unicode scalar error is correct; the
DEFAULT evaluation boundary remains a documented deviation. These two probes
are retained separately from the 404-case Unicode matrix, not counted as passes
or silently excluded from a broader compatibility claim.

Agent review followed REVIEW.md for NULL/type/error ordering, parser forms,
exact Unicode bytes, long-input work bounds, Arrow result/schema and reader
ownership, reused statements, lazy constants, DEFAULT publication and unchanged
storage semantics. Initial test corrections fixed an inverted scalar helper
return assumption and an unverified PostgreSQL DEFAULT timing expectation;
the latter produced the independently reproduced issue above. No unresolved
actionable finding remains within this bounded implementation, with #479
explicitly retained. Human approval, hosted CI and merge are not asserted.
