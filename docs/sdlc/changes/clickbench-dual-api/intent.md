# Intent: compare both public ADBC read paths

2026-10-21. Albert Wong requested both paths during the full c6a cluster run,
then explicitly chose supported scan comparisons while retaining full ClickBench
separately. Driver source stays at 9f5188c411f1e3a3bf1423a72089e2441b7995bd.

Measure ordinary statements versus native ExecutePartitions/ReadPartition on
the same full table, client and cluster, without changing the 43-query run or
pretending the native API supports global SQL operations. See [spec](spec.md)
and [plan](plan.md).
