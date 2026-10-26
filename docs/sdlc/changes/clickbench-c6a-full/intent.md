# Intent: full-scale ClickBench on c6a Redis Cluster

2026-10-21. User explicitly requested all 99,997,497 rows and expansion of the
c6a.4xlarge cluster as required, then required account-correct anti-reaper tags.
Driver source: 9f5188c411f1e3a3bf1423a72089e2441b7995bd. Harness uses working-tree
self-hosted changes. See [spec](spec.md) and [plan](plan.md).

The six-node 100,000-row pilot is insufficient as a full-scale comparison.
Run all 43 official PostgreSQL queries three times on the full source, preserving
failures and independently validating values. Count all allocated hardware;
cluster timings are not hardware-equivalent to a single c6a.4xlarge baseline.

The user also requested a reusable backup for future benchmark runs. Preserve
the older S3 snapshots and create a current-topology full-data backup with
source inputs, checksums, exact executable artifacts and restore instructions.
