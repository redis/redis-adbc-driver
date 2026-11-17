# Verification and review

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Baseline: `19ebcb4605143974495b3e83791c2909b37b7809` plus this change.
Status: implementation and verification complete; PR pending.

## Issue and upstream evidence

Fresh GitHub CLI inventory: 51 open issues, 50 in scope after the user excludes
#475. Subagents inspected assigned issue bodies/comments and source. Rechecked
#383/#476 merge metadata, eighteen documentation PR states and the still-open
Arrow, driverbase, adbi, Datasets and Airflow gates. Documentation outcomes remain
four merged, twelve open non-draft and two declined. No additional issue met its
closure gate before implementation. See [dispositions](triage.md).

#110 body was corrected in place to distinguish already-passing retained cases
from open signature/formatting scope. The original historical example table
remains labeled. MIN_SCALE advances #110; it does not complete that issue. The
Dask append implementation satisfies #273's bounded feature/acceptance scope;
its closure belongs to the merged PR. No new issue or upstream message was made.

## Current checks

- `make -C go build`: passed with the Makefile version linker flag, Go 1.27.4
  and pinned Arrow Go v18.9.0.
- `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...`: passed. Redis-dependent
  integration skips in this unit-only pass are not Redis compatibility evidence.
- `make -C go test-software` with race and the MIN_SCALE/function-registry
  selector: eight top-level tests passed, no matching skips or failures. Covers
  managed decimal256 rows, bound decimal128, strict NULLs, empty Arrow int32
  result schema, unknown literals and native input ownership.
- Native Python direct interpreter, both `python/tests/test_dask.py` and
  `python/tests/test_dask_write.py`: **29 passed, zero skips/failures/warnings**,
  3.85 s. Python 3.14.10, Dask/Distributed 2026.8.0, pandas 3.1.1, PyArrow 26.0.1.
  Two separate worker processes verify complete exact results, decimal128,
  timestamp(us), date32, bool, binary including NUL/0xff, integers above 2^53,
  Unicode/NULLs, empty partitions, native refusal, failed partition, cleanup and
  preserved prior data. Unit cases cover count/close failures, mandatory replay
  consent, duplicate recomputation and safe-conversion refusals.
- Separate PostgreSQL 18.6 oracle: thirteen MIN_SCALE query groups, including
  expected signature/input/range SQLSTATEs and explicit nonfinite exclusion.
  See [MIN_SCALE evidence](../min-scale/validation.md).
- Relative change-record Markdown links, retained oracle JSON parsing and
  `git diff --check`: passed.
- Independent subagent review found no blocking correctness/lifecycle,
  ownership/security or evidence/claim issues. A coefficient-mutation assertion,
  Dask retry-annotation scope and memory wording were corrected before the final
  checks. This is agent review, not human maintainer approval.

The owned fixture was initialized with `make -C go redis-up`; each live command
repeated `software-setup.sh --exec` management gates. Redis Software 8.8.0 with
Search, three active primaries, noeviction, trusted authenticated TLS/RESP3,
active/effective QPF factor 4, six Search workers, mnp routing and 32 connections.
Client: macOS arm64 through Docker's published-port route. Operational identifiers,
credentials, raw logs and trust material remain private.

Docker's default network pools were exhausted; an isolated explicit-subnet
Compose override allowed fixture creation without deleting existing networks.
The first Python gated command used relative paths after the setup script changed
working directory and collected no tests; the corrected absolute-path invocation
completed all 29 cases. Neither attempt is hidden or counted as a passing suite.

No full Redis integration suite, ClickBench, performance claim, sanitizer gate,
new topology or dependency upgrade was attempted. Dask worker-loss recomputation
may duplicate writes despite zero ordinary task retries; explicit consent is
required and counts exclude lost/replayed attempts. Numeric nonfinite support
remains #362. Resolver #475 was untouched at the user's direction.

See [intent](intent.md), [spec](spec.md), [plan](plan.md),
[Dask evidence](../issue-273-dask-write/validation.md).

The owned Software container/network and generated test credentials/trust were
removed after validation; pre-existing containers/networks were preserved.
