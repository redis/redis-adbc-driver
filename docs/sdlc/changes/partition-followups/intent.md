# Intent

2026-10-21; baseline `edbd299`; requested by alberttwong: work on issues
[#358–363](https://github.com/alberttwong/redis-adbc-driver/issues/358) and
[#372](https://github.com/alberttwong/redis-adbc-driver/issues/372).
Status: implemented subset locally validated; designs published; no maintainer approval.

Distributed consumers need portable bound expressions, complete reads under
Search caps, standard incremental partition discovery and reproducible evidence.
Global SQL, additional layouts and stable retries need explicit safe designs
before acceptance expands. Preserve independent authentication, application data
ownership and the live-read contract. No deployment or paid resource provisioning.

See [spec](spec.md), [plan](plan.md) and the
[operator/layout/replay design](../../../partition-execution-design.md).
