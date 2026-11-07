# Recovery evidence

Date: 2026-11-06. Base: `bbc0c0a099dba585a41dadf355a6b8324e3ead1c`.
Issue: [#345](https://github.com/redis/redis-adbc-driver/issues/345).
Status: diagnostic defect corrected; historical recovery failure unresolved.

The original runtime and proof tuple are unavailable. Its retained log confirms
the explicit aggregate error after recovery and resumed compaction, but cannot
distinguish a reply/routing problem, an expired lease or inconsistent persisted
metadata. No causal production correction is established by this work.

## Reproduced diagnostic defect

`snapshotClusterProofObserver` previously observed the maintenance client only.
Managed SQL uses a separate execution client. Its existing self-test directly
called a store lookup, so it did not establish coverage of the failing SQL path.

`TestSnapshotClusterMetadataProofObserverCoversSQLExecution` injects an incorrect
expected count only into the execution client's native proof reply. Before the
observer change it fails with `observer missed the failed SQL execution proof`.
Afterward it passes: the SQL error retains `expected=999`, the independent read
is healthy and the private proof record is saved. The fault does not mutate
Redis. This reproduces an observation gap, not the historical recovery failure.

The observer now attaches before a managed execution's first command, including
the fresh connection checked after recovery. Its bounded private records also
include mapped owner, pin, server time and write/read scope. The original
replies and errors remain unchanged. A fresh healthy proof is still only a
later observation, not evidence that an earlier proof was incorrect.

Failed owned recovery fixtures retain Redis state rather than deleting it during
test cleanup. Recovery runs fail fast and copy each attempt's diagnostic files
to a unique private archive. Archive directories use mode 0700 and files 0600;
symlinks are refused. Capture failures preserve a failing native exit status,
and cannot turn a successful test without retained artifacts into a claimed
successful orchestrated run. Further investigation must inspect a failed fixture
before running another suite that resets it.

## Checks

All backend checks used the owned Redis Software 8.2.0-78.18 fixture (Redis 8.6),
strict TLS/RESP3, Cluster API, three primary and three replica shard processes,
noeviction and verified active/effective 4x QPF. The RDB fixture disables AOF.
Every restart required all six shards' completed RDBs strictly later than the
acknowledged probes and checked all three physical-primary sentinels. This is
one-slot managed metadata/data recovery evidence, not cross-shard transactions.
Credentials, infrastructure identities, raw logs and license remain private.

| Source/check | Result |
|---|---|
| Unchanged base, original observer self-test | Pass |
| Unchanged base, primary promotion then all-node RDB restart | Pass: 6.83 s / 41.49 s |
| New SQL-path regression with original observer | Expected failure: SQL proof was missed |
| Corrected observer, both controlled differential tests | Pass; original SQL error preserved |
| Corrected observer, ten serial promotion/RDB pairs | All 20 pass; 420 matched proofs, including 180 execution proofs, zero mismatches |
| Final source, Cluster test selection | Pass; orchestrator-only recovery/checkpoint helpers skip in this selection and run separately below |
| Final source, primary promotion then fresh all-node RDB restart | Pass: 7.80 s / 42.78 s; each matches 26 proofs, including 14 execution proofs, zero mismatches |
| Host unit-only `go test -race -count=1 -p 1 -timeout=30m -json ./...`, backend URI variables unset | Six packages pass; 1,218 test/subtest passes, 1,081 backend-dependent skips; not backend compatibility evidence |
| Concurrent observer-attachment regression under host race detector | Pass |
| `python3 -m unittest discover -s scripts -p test_snapshot_software_cluster.py` | Eight offline tests pass |
| `go mod verify`; `git diff --check` | Pass |

Linux Cluster binaries were built with `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go
test -c` and executed in the fixture's independent client container. They are
not race-instrumented. The orchestration commands are
`scripts/snapshot_software_cluster.py --directory <private-directory> --project
<owned-project> --test-binary <binary> --action test`, followed by `--action
failover` and `--action restart`. Fixture preparation and each load verify
effective QPF and the persistence/replication policy. The required default
`make -C go redis-up` fixture was also provisioned with verified 4x QPF; the
Cluster checks above used their separate owned fixture.

## Assessment and remaining acceptance

Passing repetitions do not explain the original failure or satisfy the issue's
causal acceptance criteria. No production code, storage protocol, retry policy,
value/reference assertion or skip condition changes. The historical limitation
in the [recovery report](../snapshot-parity/recovery-reference-audit-validation.md)
remains applicable. The next occurrence can now be observed on the actual SQL
path with its failed fixture retained for reference/lease/persistence inspection.

Review against `REVIEW.md`: the changes are test-only and bounded; controlled
faults return their original errors; artifacts remain private; actual recovery
checks pass. No causal fix or maintainer approval is inferred from that review.
