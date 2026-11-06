# RDB checkpoint and recovery checkpoint

Date: 2026-11-03. Parent driver source f327ac0; storage protocol remains 14.
[Full feature/performance parity](plan.md) remains incomplete. The
[AOF/Cluster report](cluster14-validation.md) remains separate evidence.

## Fixture and checkpoint admission

An isolated Redis Software deployment used three nodes, three primary shards and
three replicas, strict TLS/RESP3, noeviction, Search and active effective 4x QPF
(Search workers 6, mnp, conns 32). Software image redislabs/redis:8.2.0-78.18,
database Redis 8.6.2; independent Linux ARM64 Go client with CGO disabled.
Private configuration, the supplied license, identifiers and logs stayed outside
the repository. No ClickBench or performance comparison was run.

The fixture helper now records and verifies its actual persistence mode. AOF
retains appendfsync-always. The distinct RDB fixture uses snapshot persistence,
master persistence enabled and a test-only five-second/one-write snapshot policy.
This is not a production tuning recommendation. An initial zero-write policy
was rejected by the API with HTTP 400; the live schema showed a minimum of one
write, and GET confirmed no database was created. That rejected setup is not
passing recovery evidence. The corrected policy passed effective configuration
and shard-role verification before tests.

After the recovery test stages its committed/private state and pauses, a second
Go process writes an independently generated sentinel to each of three distinct
routed primaries, without resetting SQL state. It verifies those writes and
retains the expected values in the independent client's private checkpoint file.
This dirties otherwise idle shards under the supported nonzero-write policy.

The orchestrator requests only the documented per-shard persistence INFO keys
with the REST API's
[extra_info_keys parameter](https://redis.io/docs/latest/operate/rs/references/rest-api/requests/shards/#extra_info_keys).
Default shard responses omit these values and cannot prove checkpoint completion.
Before any database container is killed, all six verified shard identities must
report AOF disabled, no RDB save in progress, successful last-save status and a
completed RDB timestamp at/after the post-pause checkpoint-write boundary. A
future checkpoint boundary is explicitly rejected. Missing/stale/failed status
cannot authorize the crash. No SAVE/BGSAVE data command is used.

Management CCS is checkpointed separately. It preserves Software's configuration,
not SQL data, and cannot substitute for the verified RDB files. The database is
fresh RDB-only storage, not an AOF deployment relabeled after the fault. A caller
requesting persistence that differs from the recorded owned fixture is rejected
before running tests. Final recovery verifies that the RDB policy survived.

## Observed results

- Normal Cluster regressions under RDB: seven passed, two explicit orchestration
  skips, zero failures. The skips are recovery and the checkpoint-only writer;
  both run in the orchestrated restart below. The final normal run also exercised
  the explicit matching RDB policy. A mismatched AOF request was separately
  rejected before test execution.
- Fresh completed RDB checkpoints with AOF disabled were verified on all six
  shards before abrupt kill/start of all three database containers. The independent
  client remained running, and persisted volumes were retained for recovery.
- Checkpoint-only three-primary writer: passed, 0.06 seconds. Recovery case:
  passed, 43.23 seconds, including checkpoint wait and full restart. This is a
  correctness-run duration, not an availability benchmark.
- All three independent sentinels recovered exactly. Committed SQL returned its
  exact 1,001 rows/aggregate values. Private 600-row directory/outcome state and
  exact endpoint values survived while its descriptor/unpublished metadata stayed
  absent from the committed manifest's shared metadata lookup.
- The same protected 64-row compaction output/checkpoint survived and resumed
  under a new owner to a complete result. The prior worker is deliberately expired
  before the fault, as in the separate AOF scenario.
- The interrupted stream returned an explicit error; its cursor was not replayed.
  This run does not establish uninterrupted streaming through RDB restart.
  Established and fresh clients subsequently read/write correctly, with zero
  explicit transient SQL read probes after deployment readiness.
- Unit-only packages: 912 pass events, 777 expected integration skips, zero
  failures. Go cross-build and Python AST parsing passed. Local checks are not
  evidence of hosted CI or native C Cluster/platform coverage.

Before cleanup, the four container identities/ownership labels were matched to
the private fixture record. Only the owned RDB project was removed. Docker
confirmed zero remaining project containers, volumes or networks. Temporary
license, server key, certificate and credential/configuration copies were deleted;
private logs remain outside git. Other projects and the normal proxy fixture were
preserved. Go formatting, Python AST parsing, local links and git diff --check
passed.

## Reproduction

Use the [owned Cluster fixture procedure](../snapshot-cluster/validation.md#reproduction)
with a new private directory, project/network and freshly built Linux Go test
binary. Select RDB explicitly when preparing it, then run normal tests and the
orchestrated restart with the same recorded policy:

```sh
python scripts/snapshot_software_cluster.py --action prepare \
  --project "$owned_project" --directory "$private_dir" \
  --license-file "$private_license" --persistence snapshot
python scripts/snapshot_software_cluster.py --action test \
  --project "$owned_project" --directory "$private_dir" \
  --test-binary "$linux_test_binary" --persistence snapshot
python scripts/snapshot_software_cluster.py --action restart \
  --project "$owned_project" --directory "$private_dir" \
  --test-binary "$linux_test_binary" --persistence snapshot \
  --test-pattern '^TestSnapshotClusterRecovery$'
```

The restart command runs the checkpoint-only writer automatically, verifies fresh
RDB completion/AOF disablement, then performs the fault. Remove only the owned
project/volumes and temporary private key/license/config copies afterward.

## Review and remaining work

Reviewed per-mode policy admission, all-shard checkpoint freshness, absent INFO
fail-closed behavior, no AOF fallback, isolated marker writes, exact post-fault
values, shared metadata isolation, compaction references, explicit stream errors
and private diagnostic handling. This is self-review, not maintainer approval.

RDB restores only a completed checkpoint. This test deliberately verifies the
checkpoint before crashing; it does not promise recovery of later unsnapshotted
writes or simulate corrupted storage. It also does not prove portable backup
import into a newly provisioned cluster. Managed data still shares one catalog
hash slot; the three sentinels prove routed shard recovery, not distributed SQL
catalog throughput. Active-Active remains excluded and noeviction required.

Full native/platform/hosted CI, process/input resume, remaining utility quotas,
sustained compaction and broader comparative performance remain open.
