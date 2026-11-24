# Plan and verification

Date: 2026-11-23. Public baseline: `760d1c8a6e91391b4ff4ceb13ec1b764fa012bd9`. Status: implementation candidate.
See [intent](intent.md), [specification](spec.md).

1. Prove large stable native targets before the existing bounded dimension rule.
2. Extract native lookup preparation and retain all independently safe unions.
3. Assert real parsed outer-Q8 graph selection and actual emitted native query,
   plus private/unpinned/selective/physical/product and cap/NULL/precision fences.
4. Parent independently reviews/composes and pilots selected Q8 with backed-up
   Software and verified active 4x QPF; full benchmark suites remain stopped.

All unit-only Go packages passed on this clean public branch:
`env -u REDIS_URI GOMAXPROCS=2 go test -count=1 -p 1 ./...`
(root package 28.096s; Redis-URI integration skips are not native compatibility
evidence). Formatting/diff checks passed. The parsed Q8 outer cost fixture preserves
the store/date/ZIP-cohort graph and predicates, with 91 dates, 6 stores, one
materialized cohort row, and a mocked 2.88-million-row published native fact. It
asserts cohort→store→date→fact and checks the actual fact Search request contains
both store and sold-date keys. Its helpers are present on this public baseline;
no unpublished streaming helper is required.

Publication is held for root's next targeted Q8 outcome. The larger measured
candidate set depends on [draft608](https://github.com/redis/redis-adbc-driver/pull/608).
No standalone native claim, benchmark restart, or remote publication follows
from these source/unit checks.
