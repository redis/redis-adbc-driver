# Managed writer handoff: intent

Date: 2026-11-14. Requestor: repository user, asking for the next performance tasks with subagents. Baseline: `d7c0978f` (merged PR #440). Status: candidate implemented; root design review approved; focused checks running.

Managed writers currently poll fresh catalog/target/server admission independently. The last target-progress pacing experiment increased command amplification and reduced throughput; it remains rejected. This experiment instead tests whether bounded, process-local FIFO turns can suppress redundant local polling and hand off eligible writes promptly. Server admission remains authoritative, including for other processes.

Success requires preserved statement/Arrow lifecycle and exact final state, lower complete-workload latency/tails or higher throughput without harmful command amplification, followed by a normal native Python/ADBC comparison before shipping any runtime candidate. No global fairness, cross-process queue, new storage protocol, cached authorization or SQL/input replay is proposed.

See [specification](spec.md) and [plan](plan.md). PR: integration pending.
