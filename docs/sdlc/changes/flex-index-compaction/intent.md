# Intent

Requested 2026-11-26 by the session working on [#658](https://github.com/redis/redis-adbc-driver/issues/658) (SSB part) and [#636](https://github.com/redis/redis-adbc-driver/issues/636): "Find out why some managed-table Search indexes on Redis Software Flex make every NUMERIC key term cost about 0.55 s per evaluation, and whether the driver's load/index path causes it." Base: main `844d8123`, then `ac82cfa0`. See [spec](spec.md), [plan](plan.md), [validation](validation.md).

[`flex-ssb-key-demand`](../flex-ssb-key-demand/validation.md#why-the-other-queries-time-out) measured, in one Flex database:
- 100 point terms cost 1.2–2.6 s on TPC-H `lineitem` but 56–57 s on SSB `lineorder`, with about 6.0M documents in each.
- `FT.INFO` layouts differed: `lineorder` (and TPC-H `orders`) had one disk key per document for every field. `lineitem` had about 272k per field.
- Both slow indexes cost about 91–93 ns per indexed document per term.
- SSB Q2.1–Q4.2 push 278–1,924 key terms, so at that cost they cannot finish within the 60 s deadline.

Outcome wanted:
- Name the cause of the slow layout, or list what was ruled out.
- If a driver load or index path produces it, fix that path with failing-first tests and rerun SSB SF1 on Flex.

Scope:
- Flex only (`search_on_bigstore`). RAM Search indexes have no disk layout.
- Read-only per-term probes (`FT.SEARCH … NOCONTENT LIMIT 0 0`) on a disposable Flex database owned by this session.
- No FCALL, RESP3, assertion or fallback changes.
