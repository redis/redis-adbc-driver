# Command wait isolation

November 13, 2026. User requested subagent performance work; parent assigned a
fresh investigation based on `06c76c0b`. Earlier dense controls measured roughly
20 ms release waits, while added INFO requests moved that wait and could not
provide per-command execution counters. Smaller fetch pages also increased
managed one-reader elapsed substantially. No common cause has been established.

Use bounded opt-in diagnostic controls to distinguish probe-command and physical
connection dependence. Production leases, release, cleanup and storage remain
unchanged. See [contract](spec.md) and [plan](plan.md). Agent findings are not human
approval; no speedup or causal attribution is claimed before measurements.

Review: [PR #434](https://github.com/redis/redis-adbc-driver/pull/434) is open and unmerged. No human approval is claimed.
