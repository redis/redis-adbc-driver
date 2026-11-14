# OLTP and selective-query efficiency

Date: November 13, 2026 (America/Los_Angeles). Source: ca6858065c4cf02418d55c3aa62eb583590fe252. Requestor: repository user. Author: Codex with delegated agents. Status: implementation. PR pending.

The measured known-address point operations have material overhead versus redis-py: managed/unmanaged reads 8.48/8.80 ms, unmanaged mutations about 10 ms and managed mutations 64–84 ms. Four-column filtered/grouped SQL also trails native Search reducers. Improve shared statement work, known-key/prepared execution, managed publication, and eligible aggregate execution using measured command/round-trip attribution. Keep snapshot/live-source guarantees, exact SQL types/values, cancellation and no replay.

[Specification](spec.md), [plan](plan.md). Evidence is workload-specific; no production SLA or product-wide ranking. No new 100M/Flex/Flight SQL or upstream issue scope.
