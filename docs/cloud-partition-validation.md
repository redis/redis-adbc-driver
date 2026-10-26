# Independent Redis Cloud Pro partition workers

Use a dedicated, disposable **Redis Cloud Pro RAM** subscription containing one
TLS-enabled database. The tests create and drop a random schema and temporary
Cloud ACL rules, roles and users. Do not use a shared or production subscription.
Obtain a price estimate before provisioning and record the owner, version,
topology, expected spend and deletion date. This runner does not provision or
delete subscriptions or databases; the resource owner must do that explicitly.

Build the native library with `make -C go build`, then use the validation Python
environment (Arrow, Redis and ADBC manager). Create a non-secret ownership record
using the exact IDs and names returned by the Cloud API:

```json
{
  "disposable": true,
  "subscription_id": 123,
  "database_id": 456,
  "subscription_name": "codex-adbc-my-disposable-test",
  "database_name": "codex-adbc-my-disposable-test"
}
```

The `codex-adbc-` prefix identifies this runner's fixtures. The tool verifies the
names, active state, RAM, TLS, database count and endpoint through the Pro API
before modifying account ACL resources. Existing principal names are never
adopted. Each created role maps its own rule to only that database.

```sh
# Each invocation reads one JSON object with account/user API keys from stdin.
# In a terminal the prompt hides input. Do not put keys in shell arguments,
# shell history, the ownership record, worker environment or committed files.
go/validation/.venv/bin/python scripts/cloud_partition_tls.py prepare \
  --config /tmp/owned-cloud.json --directory /tmp/fresh-cloud-tls

REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.so" \
  go/validation/.venv/bin/python scripts/cloud_partition_tls.py validate \
  --directory /tmp/fresh-cloud-tls --output /tmp/cloud-partitions.json
# On macOS select libadbc_driver_redis.dylib instead.
```

Preparation requires a fresh directory. It retrieves the owned default user's
password privately and writes it inside the private fixture directory. It adds
normal TLS trust roots to a generated local test CA bundle. Cloud ACL test
passwords contain upper/lowercase letters, digits and punctuation within 32 characters. The local CA signs a
wrong-host certificate for the negative test; it is not installed in OS trust.
Workers use explicit verified trust, their own read-only credentials and a fresh
process environment without API keys or planner/admin connection variables.

The checks compare complete Arrow values and schemas, Search scans and direct
HASH reads, duplicate/deleted IDs, FT.INFO/HMGET denials, CONFIG/write denials,
malformed/stale descriptors, live mutation visibility, cancellation and early
release. They also reject an unrelated CA and a correctly signed wrong-host
certificate. Cloud scans use 137 only after reading a real aggregate cap at least
that large. OSS/Software fixtures continue to require an actual cap of 137.

An installed-wheel run can use `--require-wheel` with `REDIS_ADBC_LIBRARY` unset;
the checker then requires the native library to come from the installed package.

The native driver defaults to **RESP3** for both proxy and Cluster API clients.
RESP2 remains an explicit `protocol=2` compatibility option. The evidence
collector handles both reply formats without changing the default protocol.

For sharded coverage, pass `--minimum-shards 2` to `validate`. The runner reads
the active database's current primary-shard count again before creating ACL
resources; an old fixture record or requested deployment size is insufficient.
Reports include that readback and the requested `--cluster-mode` (`auto`, the
default, `false` for the single-endpoint proxy client, or `true` for the OSS
Cluster API client). The setting applies to both planner and fresh workers.

A Cluster API run needs that API enabled with externally reachable
advertised endpoints on the owned database. It also grants the restricted worker
`CLUSTER SLOTS` and `COMMAND` discovery access when selected explicitly or
detected automatically. The checker requires a complete,
nonoverlapping 16,384-slot map, finds all 1,099 live fixture row keys through
Search, and proves they span the advertised primary owners before comparing
the independently read partition values. Reports retain per-primary row counts,
without credentials or descriptor payloads. `--minimum-shards` checks backend
shards through management metadata; `--minimum-cluster-primaries` independently
requires advertised slot owners (default 1). Cloud can expose one proxy owner
covering every slot even when management reports two backend shards. Such a run
validates the cluster client through that owner, not direct backend routing.
Successful proxy-client reads alone do not establish Cluster API compatibility.

## Cleanup and interruption recovery

The runner deletes only its fresh ACL users, roles and rules, in dependency
order, and verifies their absence by created resource ID. It atomically records
names before POST and IDs after creation, and never replays
POST after an ambiguous result. Task/creation uncertainty and cleanup errors
remain visible in `cloud-acl-cleanup.json`; they cannot produce a passed result.
An interrupted or failed journal blocks another validation until cleanup:

```sh
go/validation/.venv/bin/python scripts/cloud_partition_tls.py cleanup \
  --directory /tmp/fresh-cloud-tls
```

Keep the database active while recovering principal cleanup. Inspect unresolved
task IDs and exact owned names privately if recovery fails. After cleanup is
confirmed, delete the owned database and subscription through Cloud management,
verify resource absence, and remove the fixture's passwords/private keys.
Publish only the non-secret result, ACL cleanup report, versions, estimate and
resource deletion evidence. A single-shard run does not establish Cloud proxy
sharding, OSS Cluster API, the full platform/client matrix or performance.
