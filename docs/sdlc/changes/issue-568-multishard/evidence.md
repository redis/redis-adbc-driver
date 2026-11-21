# Bounded multi-shard coordination model evidence

Date: 2026-11-20. Code base: `bc6780e9`, merged through PR #569 as `4faa8806`.
This change is a reference model and corrected design, not a distributed driver.
[Intent](intent.md) · [Specification](spec.md) · [Design](design.md) · [Plan](plan.md)

## Executed checks

Host Python 3.15.2, standard library only, no Redis, Docker or network tests:

```sh
python3 -m unittest discover -s scripts/multishard_model -p 'test_*.py' -v
python3 scripts/multishard_model/model.py
```

All **14 tests passed**. Breadth-first exploration exhausted **3,096 states and
14,340 transitions** without an encoded visible/pinned participant violation.
The CLI also reproduced unsafe lease-based collection, stale prefix-only deletion,
and independently rolled-back central retirement. A state cap raises INCOMPLETE;
a truncated run cannot report success. The dedicated GitHub workflow runs these
pure tests on Python 3.14 without starting a Redis fixture.

The [model README](../../../../scripts/multishard_model/README.md) lists its exact
bounds: one immutable generation, two participants, one representative pin,
monotonic central decisions and local fences, no history rollback in the positive
explorer. No arbitrary-generation, multi-writer, fairness/liveness, row-batch,
Search, failover or native implementation proof follows from these results.

## Review and corrections

Two subagents implemented and independently reviewed the model and contract;
the coordinating agent reviewed and reran the final 14 tests. Findings fixed:

- Initial local prepare/seal transitions implicitly inspected current central
  state. The repair permits delayed local operations after central abort/retirement
  until a genuinely local deletion fence stops them, including deletion before
  late initialization.
- Local deletion now explicitly separates an already-issued capability's model
  provenance from local identity/fence checks. It does not pretend that local Lua
  reads remote central state; authentication remains an assumed runtime obligation.
- Successful prefix allocation originally failed to record use. It now records
  the prefix, and a sequential allocation regression rejects later reuse.

No further actionable findings remained in the declared model scope. Agent review
is not human maintainer approval. Documentation links, YAML parsing and full diff
whitespace were checked. Production protocol 17 and driver behavior are unchanged.

## Remaining issue acceptance

The model demonstrates a useful protocol direction without waiting for a native
cross-shard live-view API. It also exposes the actual unresolved durability case:
remote deletion surviving rollback of the central decision that authorized it.
Explicit unavailable reads detect missing data but do not restore durability.

Next work is new-format bounded participant batch/seal operations, verified
persistence/routing/failover contracts, publication/pins/cleanup integration,
physical multi-primary Software/Flex QPF4 validation, full query/backup correctness
and source-specific throughput/resource measurements. #568 remains open. This
change itself starts no operational ingestion or AWS resources; the separately
authorized #564–567 measurement subagent owns its local loading experiments.
