# Validation: Cluster API and recovery compatibility

Date: 2026-11-02 (America/Los_Angeles). Change based on 2734c57.
PR: https://github.com/redis/redis-adbc-driver/pull/284
See [intent](intent.md), [spec](spec.md) and [plan](plan.md).

## Environment and checks

Owned Redis Software Docker fixture on ARM64, image
`redislabs/redis:8.2.0-78.18`, effective database 8.6.2, three nodes,
three primary shards and three replicas. Management verified noeviction,
AOF `appendfsync-always`, standard 16k hash slots, all-node proxy placement,
TLS and active 4x QPF (scaling factor 4, Search workers 6, mnp, conns 32).
The Cluster API client ran in an independent container on the private network,
using multiple seed endpoints, RESP3 and certificate verification. It survived
restarts of all database containers. Operational identifiers, credentials,
certificates, logs and the supplied license remain private.

| Check | Result |
|---|---|
| Uncached Go unit suite, `go test -count=1 -json ./...` | 891 pass events, 636 expected integration skip events, zero failures |
| Proxy snapshot suite, `go test -race -count=1 -v -run '^TestSnapshot' .` | 52 pass events, six Cluster API tests skipped in that invocation, no race reports |
| Native library, `make build` | Passed on macOS ARM64, Go 1.27.2 |
| Native C/ADBC snapshot validation | Nine passed, no skips; ADBC manager 1.13.0, PyArrow 26.0.0, redis-py 8.1.0 |
| Python orchestrator syntax and Git whitespace checks | Passed |

The proxy checks used the same three-primary/three-replica Software deployment
with Cluster API disabled for that phase, still using TLS, verified active 4x
QPF, persistence and noeviction. Native checks cover the C boundary through the
proxy; Cluster API runtime checks exercise the Go ADBC implementation on Linux
ARM64. This does not claim native C Cluster API platform coverage or green full
legacy integration CI.

## Cluster API coverage

The focused tests cover managed INSERT/UPDATE/DELETE with exact integers above
2^53, ALTER, temporary isolation, manifest copying, direct-node stale-writer
rejection, rename/rekey with pinned old rows and preserved row sequence,
failed-rekey atomicity, and fail-closed behavior after metadata-directory loss.
An application HASH import spans routed primaries, preserves exact values,
remains unchanged by later application writes and leaves sources intact on DROP.
Auto-detection, live partition production/read, retained capture after segmented
DML, replay after TRUNCATE and explicit retained-result revocation are verified.

A paused FT.CREATE test reclaims its workspace/data reservation before allowing
the global command to arrive. Post-admission rejects it, the root stays unchanged,
and orphan maintenance removes the late private index. This covers the distributed
Search registry requirement without claiming the global schema command itself is
atomic with the metadata-slot lease check.

The final clean fixture's normal Cluster API invocation passed six focused tests;
its recovery test skipped there because it requires orchestration. Separate
orchestrated runs passed catalog-primary promotion (6.36 seconds) followed by
abrupt kill/start of all three database containers with their persisted volumes
(31.00 seconds). Both streams completed all 1,001 rows with exact values and no
duplicates, and the subsequent SQL readiness probe needed zero transient retries.
Established and newly opened clients read the exact aggregate values and could
write again. Unpublished metadata never appeared in the root. A 64-row compaction
output/checkpoint survived, retained its identity/position, and resumed to a
complete published result under a new owner. Compaction ownership was deliberately
expired before the fault to model a dead worker without waiting two minutes.
These durations describe this correctness run, not an availability benchmark.

## Fixture corrections and review

An initial full restart reassigned Docker node addresses and prevented Software
from starting its shards. The fixture now reserves stable private addresses.
Readiness verifies actual primary/replica roles and distributed Search indexes,
not only the management database status. Full-quorum crashes also exposed restoration of an older management configuration
(database ID/routing mode), distinct from SQL database AOF. The fixture explicitly
checkpoints each node's local CCS immediately before the crash. This saves only
Software management state; it does not SAVE the SQL database or replace its AOF
recovery. See Redis's [cluster recovery documentation](https://redis.io/docs/latest/operate/rs/clusters/cluster-recovery/)
for the separate configuration and database persistence roles. Proxy validation
is the final fixture phase; a clean Cluster API database is used for combined
promotion/restart checks.
Failed fixture/probe attempts are excluded from passing recovery evidence.

Agent review inspected correctness/lifecycle, ownership/TLS and specification/
evidence under REVIEW.md. Reproduced CROSSSLOT counter allocation and invisible
Lua-created distributed indexes were fixed and regression tested. Metadata copy
and private reclamation now use exact lexicographic directories, so restart cannot
silently invalidate a SCAN cursor. Committed directory counts reject incomplete
metadata; protocol 6 rejects earlier drafts without modification. Shared rows
remain immutable, reclaimed only after reference checks; no uncertain SQL or
cursor is replayed. All reproduced driver findings were fixed; invalid fixture attempts were
excluded and the corrected clean fixture passed. No unresolved important driver
finding was identified in this review.
This is an agent review, not human approval or a merge/release decision.

## Boundaries

A catalog still occupies one `{meta}` slot. Cluster connectivity does not shard
one catalog's rows across slots. Active-Active remains unsupported and noeviction
required. Autocommit-only behavior, no migration/downgrade and staging/reader/
segment limits remain. Interrupted operations may return explicit errors; tests
use bounded, explicit application read probes for recovery, never driver replay.
External refresh cannot atomically coordinate application writers. These runs do
not establish arbitrary simultaneous-failure, disk-corruption, cross-region,
Cloud/Flex or all-platform guarantees.

## Reproduction

Use a fresh owned Compose project and a private directory (0700), a private
license file (0600) permitting six shard processes, and Python with `redis`.
The proxy/native phase additionally needs `adbc-driver-manager` and `pyarrow`.
The supplied license contents are never part of these instructions.

```sh
export COMPOSE_PROJECT_NAME=adbc-snapshot-cluster-test
snapshot_fixture_dir=$(mktemp -d)
docker compose -f go/compose.snapshot-cluster.yaml up -d --wait
python scripts/snapshot_software_cluster.py \
  --project "$COMPOSE_PROJECT_NAME" --directory "$snapshot_fixture_dir" \
  --license-file /private/path/license.key --action prepare
(cd go && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
  go test -c -o build/snapshot-cluster.test .)
python scripts/snapshot_software_cluster.py \
  --project "$COMPOSE_PROJECT_NAME" --directory "$snapshot_fixture_dir" --action test
python scripts/snapshot_software_cluster.py \
  --project "$COMPOSE_PROJECT_NAME" --directory "$snapshot_fixture_dir" \
  --action failover --test-pattern '^TestSnapshotClusterRecovery$'
python scripts/snapshot_software_cluster.py \
  --project "$COMPOSE_PROJECT_NAME" --directory "$snapshot_fixture_dir" \
  --action restart --test-pattern '^TestSnapshotClusterRecovery$'
# Final phase; changes this disposable database to proxy mode.
python scripts/snapshot_software_cluster.py \
  --project "$COMPOSE_PROJECT_NAME" --directory "$snapshot_fixture_dir" \
  --action proxy --python /path/to/venv/bin/python
docker compose -f go/compose.snapshot-cluster.yaml down --volumes
```

Use the appropriate Linux architecture for the client container. Override the
Compose subnet and all node addresses together if Docker reports a pool conflict.
Do not reuse a shared deployment; every test clears its selected database/indexes.
Remove the generated private certificate/key, credentials and any temporary
license copy after removing the owned fixture. Preserve unrelated projects.
