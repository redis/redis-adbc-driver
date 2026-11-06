# Current-protocol Cluster and AOF recovery checkpoint

Date: 2026-11-03. Parent driver source 87179f9; storage protocol 14.
[Full parity](plan.md) remains incomplete. The older
[Cluster report](../snapshot-cluster/validation.md) tested protocol 6; its numbers
remain historical and are not relabeled as current results.

## Environment and admission

An isolated local Redis Software deployment used three nodes, three primary
shards and three replicas. Image redislabs/redis:8.2.0-78.18, database Redis 8.6.2,
Linux ARM64 independent Go client, strict TLS/hostname verification and RESP3.
Management/shard checks verified noeviction, AOF appendfsync-always, actual shard
roles/loading state, Cluster API routing, active effective 4x QPF, Search workers
6, mnp and conns 32. The supplied license and all operational state stayed in a
private directory. No unrelated Docker project was reconfigured or stopped.

The initial requested Docker subnet overlapped an existing pool; Docker rejected
it before cluster creation. A nonoverlapping private subnet was selected and
stable addresses retained across restart. Failed fixture attempts are not passing
evidence. The existing orchestrator verifies deployment policy before each run
and rechecks shard roles, QPF and distributed Search registration after faults.

## Checks and stronger assertions

The current driver passed all six existing normal Cluster cases before test
enhancement. A separate original promotion case passed in 4.97 seconds. These
confirm that the previous protocol-6-only evidence can be replaced for those
specific paths, not that the entire platform matrix is complete.

The added Cluster row-batch test injects a lost second-page reply independently
for INSERT, UPDATE, ALTER USING and DELETE. Each operation must send exactly 600
images in three bounded pages, resolve the outcome by reading and produce exact
COUNT/MIN/MAX results. Catalog and row keys must share their intended slot.

Recovery now retains a private 600-row generation and its exact last-batch
operation ID/88-row outcome in addition to the prior pinned stream, unpublished
metadata and durable 64-row compaction checkpoint. After the fault, all private
directory entries and exact values at both ends must survive, the outcome must
match and the private descriptor must remain invisible to committed SQL.

The unpublished metadata check now resolves through the committed manifest's
shared metadata buckets. Raw absence under only the current root prefix was not
sufficient evidence once native values could be shared from earlier roots.

- Final normal Cluster API run: seven passed, one explicit recovery-orchestrator
  skip, zero failures. Covers managed SQL/temporary objects, direct-node fences,
  metadata/rekey, late-index reclamation, external imports spanning primaries,
  auto detection/partition production and bounded no-replay row batches.
- Enhanced catalog-primary promotion: passed, 7.24 seconds. The pinned stream
  completed all 1,001 exact rows, with zero explicit transient SQL read failures.
- Abrupt kill/start of all three database containers with persisted volumes:
  passed, 33.37 seconds. Private row images/outcome, shared metadata isolation,
  committed exact values and compaction identity/checkpoint survived. The pinned
  stream completed all exact rows; established and newly opened clients could
  read and write again, with zero explicit transient SQL read failures.
- The compaction worker is deliberately expired before each fault to model a
  dead worker without a two-minute wait. Its existing output/checkpoint resumes
  under a new owner and publishes the complete unchanged logical result.
- Software management CCS is checkpointed on each node before full-quorum loss,
  independently of SQL database AOF. No SQL SAVE/RDB checkpoint is taken for the
  AOF recovery case. The independent client remains running during the fault.
- Unit-only packages: 912 pass events, 776 expected integration skips, zero
  failures. Unit skips do not prove deployment compatibility.

## Replicated proxy and native checks

As the final fixture phase, Cluster API was disabled for the proxy connection.
The orchestrator reverified active 4x QPF, noeviction, AOF policy and actual
three-primary/three-replica readiness before running suites sequentially.

- Full snapshot race suite through the replicated proxy: 195 pass events,
  11 explicit skips, zero failures, 217.370 seconds. Eight skips require Cluster
  connection/orchestration and are covered by their separate runs above; three
  require temporary ACL principals and remain separate from this invocation.
- Versioned native library rebuilt with make build.
- Native C/ADBC snapshot validation through that proxy: 13 passed, zero skips
  or failures, 13.777 seconds. macOS ARM64 client; this is not native C Cluster API.
- Go formatting, evidence links and git diff --check passed.

Before cleanup, container identities and ownership labels were compared with the
private fixture record. Only that project's four containers, three persisted
volumes and private network were removed. Docker inventory confirmed no remaining
containers/volumes/networks for the project. Temporary license, server key,
certificate and credential/configuration copies were removed. Private logs remain
outside the repository; the ordinary proxy fixture and unrelated projects remain.

## Review and limits

Reviewed routing, hash-slot boundaries, primary reads, private outcome durability,
shared metadata visibility, cursor non-replay, reader ownership and reference-safe
compaction recovery. This is self-review, not maintainer approval.

The Cluster Go binary is Linux ARM64 with CGO disabled; this is not a Cluster
race run or native C Cluster API/platform proof. Streams are permitted to fail
explicitly after an interruption; these particular runs completed exactly.
GitHub reported no checks on the PR branch when inspected for this checkpoint;
local validation is not evidence of green hosted CI.
Durations are correctness-run observations, not an availability benchmark.
One catalog still occupies one {meta} slot; multi-primary connectivity/imports
do not establish distributed managed-catalog throughput. Active-Active remains
excluded and noeviction required.

RDB policy/checkpoint/restore needs its own verified run; AOF restart does not
prove it. Redis Software's separate persistence choices are described in its
[persistence documentation](https://redis.io/docs/latest/operate/rs/databases/configure/database-persistence/).
Arbitrary simultaneous failures, disk corruption, cross-region behavior and the
full native/platform matrix are unverified. Process/input resume, sustained
compaction and broader comparative performance remain required by the goal.
