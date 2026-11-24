# Plan and verification

Date: 2026-11-23. Public baseline: `760d1c8a6e91391b4ff4ceb13ec1b764fa012bd9`. Status: proposed; selected Q8 validated in a combined candidate.
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

The isolated native-only planning follow-ups `0bed7ce5` and `da4f97ba` admit a
conservative 8192-row estimated dimension product for proven-large native targets.
UNION admission and each equality key set remain capped at 1000. Actual retained
metadata has five ZIP rows and 12 stores: the parsed fixture asserts date-before-
fact at 5×12×91=5460, native 8192/8193 and UNION 1000/1001 boundaries, overflow and
unsafe-scope fallback. Opt-in diagnostics assert the actual sanitized counts,
order and eligibility markers without assembly in metrics-only/opt-out mode.
All unit-only packages passed on this final state (root package 29.029s), with
the same command and native-skip limits above.


Targeted original TPC-DS SF1 Q8 passed on the combined source
`db22fcd8d833b661bcb975cd18cfdfd1d5cac0b4`, GetInfo
`v0.0.31-dsfix8`, library SHA256
`0442948bea5d1f78c72317591652952ae41a2517d0937ffd9ba371f420320778`.
One warm-up completed in 7.962310128s; three timed runs completed in
5.698030211s, 5.725762628s and 5.736935920s (median 5.725762628s).
All timed results were completely fetched and validated against DuckDB: five
rows and two columns. DuckDB median was 0.0102485s. A separate short diagnostic
confirmed input counts 2880404/91/12/5 and chosen order [3,2,1,0], ordinary
published scopes, and no OOM.

Environment: Redis Software 8.8.0, experimental Flex, verified active 4x QPF,
logical memory 64GiB/cache 8GiB; Linux ARM64 client in the Software server's
network namespace, client limit 8GiB/4CPU, server container limit 20GiB.
Engines ran sequentially against the retained original fixture, with a 60-second
statement limit and disk-backed result validation. Full suites remain stopped.
The combined candidate also includes separate streaming, parallel, scalar and
complete-candidate work. The measured Q8 candidate set depends on the larger
cap in [draft #608](https://github.com/redis/redis-adbc-driver/pull/608).
These measurements establish combined correctness/completion, not isolated
performance attribution to this PR. Issue #594 remains open.
