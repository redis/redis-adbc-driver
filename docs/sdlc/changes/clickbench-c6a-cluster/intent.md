# Intent: six-node c6a ClickBench

Date: 2026-10-21. Requestor: user in this chat. Source commit: 9f5188c411f1e3a3bf1423a72089e2441b7995bd.

Run Redis ADBC ClickBench on six c6a.4xlarge primary nodes without replicas,
with ADBC colocated on one node. Report the aggregate hardware allocation;
this is not hardware-equivalent to a single c6a.4xlarge leaderboard entry.
First measure indexed data memory requirements before attempting full scale.
See [spec](spec.md) and [plan](plan.md).
