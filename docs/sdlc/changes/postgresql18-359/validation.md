# PostgreSQL 18 mismatch inventory: validation

Date: 2026-11-09/10. Author: Codex. Requestor: Albert Wong. Status: validation in progress.
Source: `08d75d50888374b805a5c89f5bb94a46cf6fe8a9` plus the PostgreSQL 18 target and this change.
See [intent](intent.md), [spec](spec.md), [plan](plan.md), and [case dispositions](cases.md).

The original baseline was 452/651 scored passes, 199 failures, two allowed
extensions, two explicit exclusions and zero infrastructure errors. The #359
inventory contains 188 of those failures; #360 owns the other eleven. This is
a curated corpus with retained REL_16_15 PostgreSQL input provenance, not the
full PostgreSQL 18 regression suite or a SQL:2023 certification.

The implementation adds checked typed integer literals, PostgreSQL input
validation, English/compact date forms, selected scalar functions/operators,
paired statistical and bit aggregates, named/default MAKE_INTERVAL arguments,
three-argument DATE_TRUNC, volatile CLOCK_TIMESTAMP, exact numeric aggregate
return types, and corrected Unicode error states. The harness admits only a
PostgreSQL 18/C reference and compares BC/large-year Arrow values without
Python datetime range failures. UUID compares using the existing UTF-8 storage
mapping. Neither the corpus nor the runner allowlist was broadened.

Numeric AVG is Arrow decimal256 with precision 76 and scale at least 16,
preserving a larger declared input scale; bigint SUM is decimal256(76,0).
PostgreSQL's arbitrary/dynamic NUMERIC precision/scale is not fully implemented.
The index SUM guard uses the input integer type, even when SUM returns decimal,
so a rounded Search answer still falls back to authoritative row values. New
window aggregates recompute frames with cancellation checks; performance of
large sliding frames has not been benchmarked.

## Scope and review limits

Every stable case is associated with a root cause, classification, source and
owning issue in [the inventory](../../../../go/pgdiff/issue359-cases.json).
“Core-related” identifies related feature families; it is not certification of
the complete feature. Exact subfeature classification remains unverified where
explicitly noted. No 177/177 or universal PostgreSQL compatibility claim follows.
Deferred cases are excluded only from this implementation change. They remain
failing in the full corpus and open under #118's follow-up issues.

Temporal infinity is deliberately unsupported. Ordinary Arrow temporal integer
extrema are finite values, not infinity sentinels. PostgreSQL's largest timestamp
values and NUMERIC NaN/infinities need a lossless representation decision (#362).
The existing broader Arrow date32 arithmetic/storage/constructor range remains;
text input now checks PostgreSQL's narrower finite range. Full consistency is
still tracked in #93. Numeric format pictures support only C-locale
`9`, `0`, `D`, `G`, `.`, `,`, `S`, `FM`; the complete formatting language and
numeric GCD/LCM overloads remain outside the implemented subset (#110).

Review followed [REVIEW.md](../../../../REVIEW.md): exact values/types,
NULL/nonfinite/overflow behavior, window FILTER/EXCLUDE and cancellation,
index fallback, temporal boundaries, quoting, corpus provenance and claim
scope. Focused pre-fix failures and raw run logs remain private. The Software
suite caught a real rounded-bigint SUM pushdown regression; its existing
adversarial regression was retained and the input-type guard corrected.
Other old assertions for floating AVG, bigint SUM overflow, and MAKE_INTERVAL's
required seven arguments were updated after independent PostgreSQL 18 checks.
Agent review is not human approval; storage/ownership/ACL publication logic is
unchanged by these fixes. No performance claim is made.

## Reproduction

Use `make -C go redis-up`, then enter the setup's `--exec` path before Redis
checks. The gate verifies active 4x QPF, six Search workers, mnp routing,
32 internal connections and persistence policy on the exact Software database.
The fixture uses Redis Software / Redis 8.8.0, three shards, Search enabled.
The reference is PostgreSQL 18.6 with C collation, using the pinned linux/amd64
image in [run.py](../../../../go/pgdiff/run.py).

- Unit-only: `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...`.
- Software: `make -C go test-software`.
- Harness: `uv run --project go/pgdiff --python 3.14 --frozen python -m unittest discover -s go/pgdiff -p 'test_*.py'`.
- Comparison: from `go/`, `bash software-setup.sh --exec make pgdiff`.
- Strict remaining-pass check: `python3 go/pgdiff/check_issue359.py go/build/pgdiff-report.json`.
- Triage coverage only: the same command with `--triage`; it must say explicitly
  that deferred failures remain failures. This never changes runner results.

## Observed comparison and checks

The fully fetched comparison at committed implementation revision
`b35a47395375311f4969d2c1a7bdb8825d486b20` completed with **574/651 scored
passes (88.2%)**, **77 failures**, **2 allowed extensions**, **2 explicit scope
exclusions**, and **0 infrastructure errors** (655 total). The comparison returns
1 for mismatches; make translates that to exit 2. This is an expected failing
compatibility suite, not a clean pass. No formerly passing case regressed.
[Sanitized results](results.json) retain only aggregate/reference/configuration
evidence and the tested revision.

| Tier | Passed / scored |
|---|---:|
| Compatibility issues | 205 / 239 |
| Retained PostgreSQL regression subset | 329 / 371 |
| Captured dbt SQL | 22 / 23 |
| Smoke | 13 / 13 |
| Type checks | 5 / 5 |

The stable #359 cases split into **122 observed passes and 66 deferred failures**.
The eleven original #360 cases still fail. Owning issue counts below refer to
the retained 66 only; #360 also owns the retained unique-index case, without
double-counting it as one of the original eleven.

| Owner | Retained deferred cases | Reason |
|---|---:|---|
| [#361](https://github.com/redis/redis-adbc-driver/issues/361) | 19 | join/whole-row/recursive/planner/type/metadata semantics |
| [#362](https://github.com/redis/redis-adbc-driver/issues/362) | 24 | lossless NUMERIC special values and temporal range/infinity representation |
| [#363](https://github.com/redis/redis-adbc-driver/issues/363) | 2 | PUBLIC security model |
| [#364](https://github.com/redis/redis-adbc-driver/issues/364) | 7 | endpoint identity, extension scope and ordered peers |
| [#98](https://github.com/redis/redis-adbc-driver/issues/98) | 9 | logical JSON type/operators/functions |
| [#107](https://github.com/redis/redis-adbc-driver/issues/107) | 2 | enum catalog/type identity |
| [#110](https://github.com/redis/redis-adbc-driver/issues/110) | 2 | OVERLAPS and diagnostic records |
| [#360](https://github.com/redis/redis-adbc-driver/issues/360) / [#114](https://github.com/redis/redis-adbc-driver/issues/114) | 1 | unique-index integrity enforcement |

Five deferred cases are Core-related, fifteen optional-standard-related,
fifteen PostgreSQL-specific, twenty-four representation/comparison limitations,
and seven fixture/oracle cases. These are case classifications, not feature
counts. Exact standard subfeature classification remains unverified where noted.

- Full unit-only Go pass completed with zero failures; integration skips are
  expected without REDIS_URI and are not live compatibility evidence.
- All **32 harness/completion-gate tests passed**. The provenance import checks
  regenerated the same 371 PostgreSQL queries and 23 captured dbt cases.
- Strict #359 pass gate correctly exits 1 with 66 unresolved failures. The
  separate triage gate exits 0 with 188/188 accounted for, preserving failures.
- Local Markdown file links and `git diff --check` passed.
- Clean final full Software rerun at `b35a4739`: pending. The first sweep exposed
  old numeric/arity assertions and the corrected integer SUM index guard; it is
  not reported as a passing validation run.

[PR #366](https://github.com/redis/redis-adbc-driver/pull/366) delivers this change.
Closing #359 completes its documented fix-or-classify triage, not #118 or the
remaining feature implementations.
Credentials, endpoints, infrastructure identifiers, license files, and raw
operational artifacts stay private in ignored build or temporary storage.
