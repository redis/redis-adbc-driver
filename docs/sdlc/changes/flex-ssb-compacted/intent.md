# Intent

Requested 2026-11-26 (UTC) by the session working on [#658](https://github.com/redis/redis-adbc-driver/issues/658) for the user: "Rerun SSB SF1 on Redis Software Flex for #658, with every table index verified as compacted before timing. Add a gate to the harness so Flex timings never start on an uncompacted index." Base: main `ac82cfa0` (includes #690 and #691), with the [`flex-index-compaction`](../flex-index-compaction/validation.md) record (`0a955736`). See [spec](spec.md), [plan](plan.md), [validation](validation.md).

Background:
- [`flex-ssb-key-demand`](../flex-ssb-key-demand/validation.md) (#691) validated 4 of 13 SSB SF1 queries on Flex. Q2.1–Q4.2 timed out, each pushing 278–1,924 key terms into `lineorder` at about 0.55 s per term.
- [`flex-index-compaction`](../flex-index-compaction/validation.md) found the cause: those indexes had not been compacted by the Flex Search disk GC. A compacted SF1 `lineorder` costs about 11.5 ms per term, so those demands should take about 3–22 s per Search evaluation.
- The first GC cycle came 5.9–9.4 minutes after `FT.CREATE` on every index tried. Clients cannot start it on Redis Software. The earlier server's indexes stayed uncompacted for hours, for reasons not established.

Outcome wanted:
- A harness gate in `benchmarks/olap-release/`: on a Flex profile, wait (bounded) until each loaded table's index shows a completed GC cycle and fewer disk keys than documents on its NUMERIC fields; refuse to time otherwise; record both values.
- An SSB SF1 Flex result table for all 13 queries with each table's compaction state, compared with the 2026-11-26 run.

Scope change, the same day:
- The strict gate refused this fixture, because SSB `date_` (2,556 rows) and `supplier` (2,000) got no first GC cycle.
- The requesting session then chose "an opt-in, recorded exemption for indexes of 10,000 docs or fewer, with the default gate staying strict". It set conditions: record each exempt index's counts, minutes since `FT.CREATE` and one point-term `FT.PROFILE`, in metadata and in every query record. Drop the exemption for an index that compacts before timing. Report the no-cycle observation as a finding.
- See [spec](spec.md#harness-gate) and [validation](validation.md#small-indexes-without-a-first-cycle).

Scope:
- Harness and documentation only. No driver change: the driver build is main `ac82cfa0`.
- Flex only. A RAM profile is not gated.
- A disposable fixture owned by this session, removed afterwards.
