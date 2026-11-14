# Reproduction

Apply candidate.patch to a clean isolated baseline `06c76c0b` checkout with
`git apply --check` followed by `git apply`. It contains only test diagnostics,
not a production optimization. Original frozen source is retained on
codex/command-wait-isolation. Record your new source/toolchain and every attempt.

Provision only an owned dedicated Redis Software fixture using
`make -C go redis-up`. Independently configure and verify the actual one 4-GiB
noeviction shard, active/effective 4x QPF / six workers / mnp32, four-CPU/eight-GiB container,
strict data TLS / RESP3. Do not assume setup defaults match these recorded limits.
Privately load REDIS_URI / REDIS_SNAPSHOT_URI / SSL_CERT_FILE and the verified
retained-control policy; credentials and generated TLS material stay private.
The existing snapshot harness clears the explicitly disposable database/indexes.

From go/, with Go 1.27.3 and GOMAXPROCS=4, first run the exact endpoint-free
`go test -count=1 -p 1 -json -run '^TestCommandWaitIsolation$' -timeout=3m .`
and repeat with `-race`, retaining both full outputs and zero skips/failures.

For each mode in this fixed order, run a new subprocess and retain every output:
control, same_ping, separate_time, pacing_20ms, separate_ping, same_time, control.
Label the first/last controls separately. Set the opt-in environment:

```sh
export REDIS_SNAPSHOT_COMPACTION_PROFILE=true
export REDIS_SNAPSHOT_COMPACTION_DENSE_PROFILE=true
export REDIS_SNAPSHOT_COMPACTION_RELEASE_IO=true
export REDIS_SNAPSHOT_COMMAND_WAIT=control
```

Change the last exported mode for each job in the fixed order above.
Run `go test -count=1 -p 1 -json -run '^TestSnapshotCompactionPhaseProfile$/^paced_false$' -timeout=5m .`.
Verify after each process that all all 20K target values/unrelatedCOUNT/MIN/MAX
checks completed, exactly 317 releases (314 copy / one index / two install) were retained,
all original releases succeeded with one Read/Write and zero pool misses/timeouts, and
probe/pacing validity agrees with actual physical connection ordinals. A failed
diagnostic assertion is not a passing test; retain it. Never rerun to discard it.
If full data or real release checks fail, stop and inspect before further work.

After every process exits, the exclusive fixture owner resets that owned database
and verifies empty/index-free, active/effective 4x QPF and strict TLS / RESP3. Join/close
all clients before loan return; only the infrastructure owner performs final
container/network/TLS/credential teardown. No ClickBench, packet capture, plaintext
transport or server/network configuration change is part of this experiment.
