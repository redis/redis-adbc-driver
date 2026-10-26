# Redis Cloud Pro validation evidence

Date: 2026-10-21. Source: 35ddb9b (merged #402); Go runtime unchanged.
Requestor and resource owner: Albert Wong. Agent: Codex. Human review pending.
See [intent](intent.md), [spec](spec.md), [plan](plan.md), and
[deployment metadata](evidence/deployment.json).

## Deployment and trust

The requestor authorized a new disposable database and then specified Redis Cloud
Pro as the default. A temporary Essentials database and subscription created
before that correction were deleted; subscription absence was verified with HTTP
404. The replacement Pro deployment uses AWS us-west-2, one primary shard through
the proxy, 0.1 GB RAM, 1,000 requested operations/second, no replication or
persistence, and no eviction. No shared databases were tested or modified.

The Cloud API dry run lists MinimumPrice $0.274/hour and one Shards item at
$0.043/hour. These are estimate components, not an invoice or measured total.
Redis 8.6.2 and Search 8.6.11 were read from the live server. TLS is enabled;
clients verify the chain using normal OS roots and verify the hostname. A socket
connection using an unrelated hostname failed certificate verification. No trust
store was changed and no verification bypass was used. The rebuilt native driver
also rejected an unrelated explicit CA with an x509 unknown-authority error; see
[negative TLS evidence](evidence/negative-tls.json). See the
[Redis Cloud TLS documentation](https://redis.io/docs/latest/operate/rc/security/database-security/tls-ssl/).

## Native partitions

macOS arm64; Python 3.13; ADBC manager 1.12.0; Arrow 26.0.0. Both native libraries
were built with Go 1.26.6 for macOS 12. The initial nine-test run used
`initial_library_sha256`; the rebuilt library uses `library_sha256` in deployment
metadata. Neither run changes the Go runtime source.
The server's `search-max-aggregate-results` was 2,147,483,648; native tests used a
verified lower bound of 137 to exercise bounded scans.

With the dedicated `rediss://` URI supplied privately and
`REDIS_ADBC_LIBRARY` selecting the recorded library:

```sh
cd go/validation
REDIS_PARTITION_SCAN_CAP=137 .venv/bin/python -m pytest -q -ra \
  --junitxml=/tmp/native-partitions.xml tests/test_partitions.py
python3 ../../scripts/check_partition_junit.py /tmp/native-partitions.xml
```

All **9 native partition tests passed**, zero skips, in 61.94 seconds. The required
partition gate also passed. Exact Arrow schemas/values, cross-process reads,
bound parameters, malformed/empty descriptors and balanced partitions use the
existing assertions without modifications. See [JUnit](evidence/native-partitions.xml).

## Full configured validation

The first full run produced 370 passed, 8 skipped and 3 passing subtests, with
one GetInfo version assertion failure. The temporary library was built without
the Makefile version linker flag and reported `(unknown or development build)`,
which upstream rejects. See [initial JUnit](evidence/full-validation-initial.xml).
No assertions, skips or driver source were changed to resolve this failure.

Rebuild using the exact command in deployment metadata (normal Makefile version
flag plus matching macOS 12 C/compiler/linker targets), then run:

```sh
cd go/validation
REDIS_PARTITION_SCAN_CAP=137 .venv/bin/python -m pytest -q -ra \
  --junitxml=/tmp/full-validation.xml tests/
python3 ../../scripts/check_partition_junit.py /tmp/full-validation.xml
```

The rebuilt library passed: **371 tests and 3 subtests passed, 8 skipped, zero
failures/errors**, in 344.25 seconds. All 9 required native partition tests passed
inside this full run. See [full JUnit](evidence/full-validation.xml).

Skips are the existing unsupported connection/ingestion capabilities (5),
GetStatistics (1), secondary catalog (1), and transactions (1). They were not
expanded for Cloud. These results cover the configured Python suite, not a full
Cloud Go integration suite or installed-wheel Cloud matrix.

## Cleanup and review

The Pro database and subscription were deleted after validation. Both individual
resource GETs returned HTTP 404, with subscription absence verified at
2026-10-21 20:50:18 UTC. The earlier Essentials resources are also absent. Local
test credentials and the negative-test CA private key were removed. See
[cleanup evidence](evidence/cleanup.json). Actual invoiced cost was not yet
available; no continued test deployment was retained.

Review: no actionable findings in the documentation/evidence diff. Local links,
JSON and XML parsing, credential exclusion, required partition JUnit gates and
`git diff --check` passed. No runtime source, assertions or skip policy changed.
Human review and new-PR hosted checks remain pending.

## Scope and remaining gaps

This run uses the default identity for both planner and worker connections.
Independent Cloud ACL identities, Cloud proxy sharding/OSS Cluster API, Linux
Cloud clients and the remaining Python/ADBC manager matrix remain untested here.
No Cloud performance measurements were made. Issue #363 remains open.
