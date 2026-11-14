# Intent

Date: November 14, 2026. Requested by Albert Wong; author: Codex point-read
subagent. Status: prototype, pending validation and performance acceptance.
[Specification](spec.md), [plan](plan.md), [validation](validation.md).

PR #439 point reads retain eleven foreground exchanges and about 2.9 ms latency
on its recorded fixture. The committed root map is loaded separately after its
acquisition proofs. Queue a bounded read-only root-map fetch with those proofs,
then consume its exact reply through the existing per-pin metadata resolver.

Preserve native authorization, protocol 17, full lease/presence/ownership proofs,
unknown-reply diagnostics, shared views and source membership. No time-based,
lease, value, authorization or cross-statement metadata cache. No storage change,
ClickBench or claim that fewer exchanges proves a wall-time improvement.
