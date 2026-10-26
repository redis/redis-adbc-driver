# Implementation and verification

Date: 2026-10-21. Status: local verification complete; human approval pending.
See [intent](intent.md) and [spec](spec.md).

1. Prepare verified API/proxy TLS on an ownership-checked Software fixture.
2. Share the native checker with OSS and Software principal managers. Wait for
   management convergence, not SQL result retries; retain cleanup failures.
3. Add both Software topologies to amd64/arm64 PR gates and Linux installed-wheel
   release gates, after existing non-TLS validations.
4. Exercise one and three actual primary shards locally. Three-shard installed
   macOS wheel: 16 checks passed. One-shard installed macOS wheel: 16 passed.
   Redis Software image 8.2.0-78.18, Redis 8.6.2, Search 8.6.11, PyArrow 26.0.0,
   Python 3.12.15 / ADBC manager 1.12.0. Runtime wheel is the verified #399 runtime.
5. Shared OSS checker regression: 15 checks passed. Three fault tests passed:
   exact ownership validation before mutations, bounded/rejected PUT retries
   without POST replay or secret output, and cleanup after partial failure while
   preserving other roles. actionlint, Python compilation and diff checks pass.
6. Record Linux arm64 packaged result and final JSON evidence; hosted amd64 and
   arm64 results remain pending. No Cloud, independent-node failover or production
   benchmark result is claimed.

Initial development exposed 409 BDB-update overlap, delayed user authentication,
and Software's filtered-empty CONFIG GET policy. Those are now explicit fixture
readiness/permission contracts; incorrect SQL results were never retried.
The unsupported in-place three-to-one reshard attempt was discarded; the owned
database was recreated with one shard for that independent topology check.

Final packaged evidence:

- [macOS, three shards](evidence/macos-three-shards.json): all 16 checks passed.
- [macOS, one shard](evidence/macos-one-shard.json): all 16 checks passed.
- [macOS, minimum manager 1.8.0](evidence/macos-minimum-client.json): all 16
  checks passed against the one-shard proxy. A fresh public dependency refresh
  with the fixture SSL_CERT_FILE unset also passed; see the #401 fix below.
- [Linux arm64 installed wheel](evidence/linux-arm64-one-shard.json): all 16
  checks passed on the one-shard proxy, Python 3.12.15 / manager 1.12.0. Built with
  Go 1.26.6 using the existing AlmaLinux 8 builder/cache, highest required GLIBC
  symbol 2.17. Wheel tagged manylinux_2_28_aarch64. Linux client shared only the
  disposable Software container's network namespace; API 9443/data 12000 use the
  same verified certificates. No Cloud deployment is represented by this test.
- [shared OSS regression](evidence/oss-checker-regression.json): all 15 checks
  passed. No result checks skipped in any of these selections. No Go runtime
  changed, so the complete Go suite was not repeated for these script changes.

Reproduction after the existing owned setup, using its actual localhost ports:

```sh
python scripts/software_partition_tls.py NEW_DIRECTORY --container OWNED_CONTAINER \
  --api-port API_PORT --db-port DATA_PORT --shards 1
# Set REDIS_TLS_ADMIN_URI from NEW_DIRECTORY/admin.uri and SSL_CERT_FILE to its
# ca.crt locally; never print those credentials or upload the private directory.
python scripts/check_partition_tls.py --require-wheel \
  --software-fixture NEW_DIRECTORY --output FRESH_RESULT.json
```

#401 follow-up: merged #399's macOS job passed 15 TLS checks, then uv could not
verify PyPI because SSL_CERT_FILE still selected the private fixture CA. Unset
that one variable for the manager 1.8.0 dependency installation; verification stays
enabled for public downloads and the native fixture. Hosted evidence:
https://github.com/alberttwong/redis-adbc-driver/actions/runs/39663698840/job/119443037044

Review inspected container/port ownership before mutations, bootstrap versus
verified API access, exact retry boundaries, partial resource cleanup, explicit
Software CONFIG policy, worker environment isolation and JSON-only artifacts.
No additional actionable finding. This agent review is not maintainer approval.
Hosted amd64/arm64 CI remains pending; Cloud and performance evidence stay in #363.
