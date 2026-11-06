# Specification: Software Flex evidence and scope

Date: 2026-11-05. Source: main `43f4f061`. Author: Codex.
Inputs: [intent](intent.md). Verification: [plan](plan.md).
Status: documentation prepared for review; maintainer acceptance not recorded.

## Contracts

- README and guide identify experimental `adbc.redis.flex=true` support and the
  validated retained replay, TRUNCATE and bounded rename/rekey paths.
- Label earlier validation/benchmarks as historical. Name `a121842` and the tested
  Software builds/topologies for the PR #331 checkpoint. Preserve six native Flex
  failures, the populated-database replica-loading failure and evidence limits.
- Explain the latest 6.9x/3.1x latency ratios as ratios of arithmetic means of
  seven workload medians. Show their mean latencies and explain cross-run
  RAM-default variability without assigning an unmeasured cause.
- Retain the distinction between positive backing-store counters and unmeasured
  cold physical-SSD amplification, and between planned failover and host outages.

## Acceptance evidence

Check all 360 raw warm samples against all 72 summary groups, recompute the
headline values, verify published native/failover metadata, and check links,
anchors, whitespace and the documentation-only diff. No live suite is rerun.
