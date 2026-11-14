# Archived experiment reproduction

Use an isolated checkout of baseline `88774f10` and an owned Redis Software
fixture provisioned through `make -C go redis-up`, verified active/effective 4x
QPF, strict TLS/RESP3 and the resource/settings contract in the report. Configure
REDIS_URI / REDIS_SNAPSHOT_URI / SSL_CERT_FILE privately, plus the verified retained
control persistence declaration. This suite is destructive to its dedicated DB.

Apply initial.patch only for the historical initial attempt; apply corrected.patch
only for the final corrected experiment. Both are complete test-only diffs from
the same baseline and remain unapplied to ordinary source. Do not combine them.
Preserve their exact bytes and every new source/run identity and raw attempt.

From go/, use Go1.27.3, GOMAXPROCS=4:

```sh
env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -race -count=1 -p 1 -json -run '^TestSnapshotCompactionCommandstats' -timeout=3m .
```

For the dense original control, set REDIS_SNAPSHOT_COMPACTION_PROFILE=true,
REDIS_SNAPSHOT_COMPACTION_DENSE_PROFILE=true and
REDIS_SNAPSHOT_COMPACTION_RELEASE_IO=true. For a separate instrumented job also
set REDIS_SNAPSHOT_COMPACTION_COMMANDSTATS=true, then execute:

```sh
go test -count=1 -p 1 -json -run '^TestSnapshotCompactionPhaseProfile$/^paced_false$' -timeout=5m .
```

The first source's Go exit0 hides invalid diagnostic samples; inspect every
commandstats_valid flag. The corrected source intentionally fails after complete
job/result validation if any per-release counter pair fails the exact-one-call
gate. Do not disable or relax that assertion or relabel the attempt as passing.
The current evidence shows this counter visibility is unusable for attribution.
No production cleanup bypass, replay, consistency change or performance default
is proposed. Finally close all clients/processes and, under fixture ownership,
remove test indexes/data, reverify effective configuration and return the loan
empty/index-free. Infrastructure and credential teardown belongs to the owner.
