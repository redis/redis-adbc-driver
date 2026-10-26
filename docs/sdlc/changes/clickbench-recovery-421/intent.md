# Intent: recover and correct full-scale ClickBench failures

2026-10-21. Requestor: Albert Wong, “fix it” on [issue #190](https://github.com/redis/redis-adbc-driver/issues/190).
Initial starting revision: 523adb1d75756b7a92a72820effb7e6949a07809.
The fixes are rebased onto main 0a84794 for separate PR #425; merged PR #403
retains its original harness scope.

Prevent a failed scan workload from passing the completion gate, retain safe
diagnostics, fix missing projection/sort inputs in parallel top-K execution,
and bound Search sort working sets for streamed row-ID reads. Recover the
lost benchmark shard and preserve original results separately from new runs.
See [spec](spec.md) and [plan](plan.md). A new full comparison or restore pass
requires actual evidence; execution success alone is insufficient.


2026-10-21 scope update from Albert: rerun only incorrect, failed or mismatched queries. Stop the full fixed sweep and preserve its completed trials. Run Q24/Q25/Q27 three times each (nine trials), independently validate all nine outputs, then rerun the previously failed 18 supported-scan measurements. Retain original full evidence separately; do not claim a complete fixed-binary sweep or mix binary timings into a full score. Complete backup, approved restore checks and cleanup remain required.


Scan timeout recovery: a full-data statement scan reached its1,200-second cap while making progress without memory pressure. Preserve that failed attempt and partial Arrow output, let its already-started partition partner finish unchanged, then resume only missing/failed measurements with a1,800-second cap. Verify source/binary/case identity on resume, retain successful measurements and independently revalidate each pair. The overall scan deadline and backup/restore reserve are unchanged.
