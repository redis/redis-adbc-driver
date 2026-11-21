# Bounded compaction throughput: intent

Date: 2026-11-20. Requestor: Albert T. Wong. Author: Codex.
Source: `1b5918e6f1e72ee6e5659efd72547e58108d21a4`.
Issue: [#565](https://github.com/redis/redis-adbc-driver/issues/565).
Status: compatible implementation; evidence and remaining acceptance tracked in [plan](plan.md).

## Problem and outcome

Wide Flex loads can accumulate segments while maintenance repeatedly acquires
and validates catalog metadata around 64-row copy batches. The historical
105-column observation included concurrent indexing and is not an isolated
compaction rate or a demonstrated cause. Reduce repeated control work for
larger copy jobs, preserve bounded atomic outcomes, and measure actual changes.

## Scope and exclusions

Amortize catalog validation over a bounded burst of existing atomic copy batches.
Keep persisted protocol-17 outcomes at 64 rows rather than silently widening
them to 128/256 (older workers reject such records). No protocol migration,
input replay, foreground compaction or raised segment admission limit. No full
ClickBench reload, AWS deployment or resumed operational ingestion.

[Specification](spec.md) · [Plan/evidence](plan.md)
