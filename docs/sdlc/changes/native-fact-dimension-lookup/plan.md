# Plan and verification

Date: 2026-11-23. Public baseline: `760d1c8a6e91391b4ff4ceb13ec1b764fa012bd9`. Status: implementation candidate.
See [intent](intent.md), [specification](spec.md).

1. Prove large stable native targets before the existing bounded dimension rule.
2. Extract native lookup preparation and retain all independently safe unions.
3. Assert real parsed outer-Q8 graph selection and actual emitted native query,
   plus private/unpinned/selective/physical/product and cap/NULL/precision fences.
4. Independently review and validate selected Q8 with backed-up Software and
   verified active 4x QPF; full benchmark suites remain stopped.

All unit-only Go packages passed on this clean public branch:
`env -u REDIS_URI GOMAXPROCS=2 go test -count=1 -p 1 ./...`
(root package 29.029s; Redis-URI integration skips are not native compatibility
evidence). Formatting/diff checks passed. The parsed Q8 outer cost fixture preserves
the store/date/ZIP-cohort graph and predicates, with 91 dates, 12 stores, five
materialized cohort rows, and a mocked 2.88-million-row published native fact. It
asserts cohort→store→date→fact and checks the actual fact Search request contains
both store and sold-date keys. Its helpers are present on this public baseline;
no unpublished streaming helper is required.

Publication is held for root's next targeted Q8 outcome. The larger measured
candidate set depends on [draft #608](https://github.com/redis/redis-adbc-driver/pull/608).
No standalone native claim, benchmark restart, or remote publication follows
from these source/unit checks.

The isolated native-only planning follow-ups `0bed7ce5` and `da4f97ba` admit a
conservative 8192-row estimated dimension product for proven-large native targets.
UNION admission and each equality key set remain capped at 1000. Actual retained
metadata has five ZIP rows and 12 stores: the parsed fixture asserts date-before-
fact at 5×12×91=5460, native 8192/8193 and UNION 1000/1001 boundaries, overflow and
unsafe-scope fallback. Opt-in diagnostics assert the actual sanitized counts,
order and eligibility markers without assembly in metrics-only/opt-out mode.
All unit-only packages passed on this final state (root package 29.029s), with
the same command and native-skip limits above. Publication remains held for the
next targeted Q8 outcome.
