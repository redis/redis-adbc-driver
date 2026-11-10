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

Final counts and per-check outcomes will be recorded after the live run.
Credentials, endpoints, infrastructure identifiers, license files, and raw
operational artifacts stay private in ignored build or temporary storage.
