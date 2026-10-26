# Independent partition workers across real TLS primaries

Use a new local disposable OSS Cluster fixture. This complements Cloud's
externally advertised proxy owner: it exercises three distinct TLS endpoints,
node IDs and physical row owners. No paid Cloud resources are created.
RESP3 remains the default.

```sh
bash scripts/start_partition_tls_cluster.sh \
  /tmp/fresh-partition-cluster redis-partition-cluster-tls 16394
export REDIS_TLS_ADMIN_URI="$(cat /tmp/fresh-partition-cluster/admin.uri)"
export SSL_CERT_FILE=/tmp/fresh-partition-cluster/ca.crt
# Select the already-built native library, or unset this for an installed wheel.
export REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.so"
go/validation/.venv/bin/python scripts/check_partition_tls.py \
  --oss-cluster-fixture /tmp/fresh-partition-cluster \
  --minimum-cluster-primaries 3 --output /tmp/fresh-cluster-result.json
```

Use the `.dylib` on macOS. For a packaged run, unset `REDIS_ADBC_LIBRARY`, select
the environment containing the installed wheel and add `--require-wheel`.
The fixture requires Docker, OpenSSL and Python 3. It binds three consecutive
client ports to host loopback; choose an unused first port between 1024 and
55533. Cluster-bus ports remain inside the container. All node client ports
require TLS and authentication; persistence and replicas are disabled.

A new private CA signs a serverAuth leaf for localhost/127.0.0.1 and a separate
clientAuth leaf for cluster peers. `tls-cluster yes` encrypts node-to-node
connections; see [Redis TLS configuration](https://redis.io/docs/latest/operate/oss_and_stack/management/security/encryption/).
The fixture never installs trust roots globally or bypasses verification.

The checker requires exactly the declared three loopback primary endpoints,
matching live node IDs, ready cluster state and an actual aggregate cap of
137 on every primary. It refuses existing principal names. Fresh planner and
restricted worker users are installed on all primaries; FT.INFO/HMGET permission
changes and CONFIG/write denials apply to every node. Partial setup and cleanup
failures remain visible. Cleanup verifies all owned users absent on every primary
and records only names/counts/status in `cluster-acl-cleanup.json`.

All 16,384 slots must be covered without overlaps or gaps. The checker finds
exactly 1,099 distinct live fixture row keys, maps them to slot owners, and
requires rows on all three. It independently verifies each key's presence
through a direct connection to its expected primary using nontransactional
single-key EXISTS pipelines. A redirect or missing row fails the check.
Complete Arrow values and schema then come from fresh restricted native worker
processes after the planner closes, alongside the existing lifecycle/denial
and native TLS negative checks.

The same invocation also migrates one slot containing only this run's row keys.
Two fresh worker processes first read a pre-planned direct-HASH descriptor on
persistent native connections. The administrator puts the slot into the legacy
IMPORTING/MIGRATING states and moves its keys over TLS. A direct source probe
must return `ASK`; with `ASKING` denied on every primary the native read must
fail explicitly, then return exact Arrow values/schema after permission is
restored. After assigning the destination owner on all three primaries, the
source must return `MOVED` and both existing connections must read the same
descriptor successfully. One connection remains idle between its original read
and the finalized move. Each child response has a 30-second deadline.

This confirms that read-only Cluster API worker ACLs need `+asking` in addition
to topology discovery and data read commands. It grants no write/admin commands.
Migration checks reject changed primary identities, unrelated slot keys and
unbounded key lists before mutation; ownership finalization refuses any keys
remaining on the source. The protocol follows Redis's
[manual slot migration procedure](https://redis.io/docs/latest/commands/cluster-setslot/).

Search coordinators can briefly report `Query requires unavailable slots` while
their topology converges. The global aggregate count can also be correct while
a later native cursor omits the migrated row. A bounded read-only fixture probe
requires both the exact 1,099-row count and exact cap-bounded complete HASH loads
on every primary for three consecutive rounds before continuing. A SORTABLE-only
row-ID load can still report cached membership while the old shard cannot load
the HASH. The probe uses `LOAD @__key` followed by `LOAD *`, including the
authoritative `__rowid`, matching native whole-row scans. Binary payloads remain
undecoded. This gate also checks initial fixture readiness. Unexpected
probe cursors are closed. It retries only that specific availability error or
incomplete/duplicate membership; other errors and the deadline fail.
Native reads/writes are not retried by the checker. After convergence, fresh
restricted processes must return every original Search value/schema, and direct
EXISTS probes must verify all rows on their new physical owners. The report
includes slot/endpoints, observed redirects and convergence attempts, without
credentials or descriptor bytes. Native mismatch diagnostics retain only fixture
row IDs/counts, schema equality and mutation counts, never raw field values.

The invocation also validates retained global SQL and typed immutable replay
after planner closure and source DROP. Its separate worker has only PING, TIME,
GETRANGE, topology/ASKING commands and read access to the fresh result namespace.
Cross-tenant access is denied; cancellation and early release leave the shared
result replayable, and explicit revocation stops both independent workers.
For retained routing, a second migration moves only that namespace's keys. Two
persistent worker connections must follow ASK/MOVED with exact Arrow output;
denying ASKING fails replay. New source capture during active migration must
reject publication. The checker finalizes ownership and removes the owned
namespace and principals before reporting success.

Always remove the owned fixture after a run, including failure:

```sh
# Verify the fixture label before removing this exact caller-owned name.
# Preserve only result JSON and cluster-acl-cleanup.json before deleting secrets.
test "$(docker inspect -f '{{index .Config.Labels "codex.fixture"}}' redis-partition-cluster-tls)" = partition-tls-cluster && \
  docker rm -f redis-partition-cluster-tls && rm -rf /tmp/fresh-partition-cluster
unset REDIS_TLS_ADMIN_URI SSL_CERT_FILE REDIS_ADBC_LIBRARY
```

The Test workflow runs native-library checks on both Linux architectures;
Release runs the same checks from installed Linux wheels and uploads only the
result and ACL cleanup JSON. This covers one fixed-data manual slot migration;
it does not establish replica failover, concurrent-write resharding, automatic
mid-stream recovery, replica consistency, stable snapshots or production performance.
