# Long managed ClickBench streams: specification

Date: 2026-11-20. Source: 1b5918e6. Status: implemented; validation limits recorded.
Issue: https://github.com/redis/redis-adbc-driver/issues/566

## Contract and boundaries

Coordinator DDL precedes 1..4 independent worker streams. Partition requested source row-group slices disjointly and exhaustively. Never replay uncertain input or equate logical IDs with source ordinals. Validate native count/unique IDs and independent typed canonical samples. Track source EOF and published acknowledgments separately; internal staging/index/maintenance phases stay explicitly unavailable. Backup readiness remains false.

See [intent](intent.md) and [plan/evidence](plan.md).
