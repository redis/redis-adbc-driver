# Installed-wheel client matrix evidence

2026-10-21. Source: 053e638 (merged #413). Requestor: Albert Wong. Agent: Codex.
Human review and new hosted matrix execution pending. See [intent](intent.md),
[spec](spec.md), [plan](plan.md), [summary](evidence/summary.json), and
[workflow contract checks](evidence/workflow-contract.json).

## Change

The Release workflow now reuses its three native wheel build artifacts in
12 independent cells: Linux x86_64/ARM64 and macOS ARM64, Python 3.13/3.14,
and ADBC manager 1.8.0/1.12.0. Each cell installs the packaged driver and exact
manager version in a fresh environment, then runs the complete existing TLS
partition checker with `--require-wheel` and no library override. RESP3 remains
the default. Fixtures are local and disposable; no Cloud account credentials
or paid deployment are needed. Existing parent jobs retain full Go/upstream
ADBC validation. This matrix is partition-specific, not the full upstream
suite repeated for every version.

Go changes now trigger the Release workflow as well as packaging/harness changes,
so the new gate also checks driver changes. GitHub release asset publication
waits for all matrix cells. Downloading only the Linux package artifacts and
the macOS package artifact keeps diagnostic JSON/XML out of binary release
assets. Historical selected refs without the checker/native fixture helper
retain their existing gates and skip these added steps explicitly.

## Local execution

Rebuilt the current source once per locally tested platform, packaged it using
`scripts/build_python_wheel.py`, and reused that wheel for four fresh client
environments. Each used Arrow 26.0.0 and redis-py 8.1.0; actual interpreter and
library hashes are in the JSON records. Python 3.14 with ADBC 1.8 built the manager
from source on macOS, so those runners need a compiler (available on the selected
GitHub hosts). The Redis driver itself always came from the installed wheel.

| Platform and server | Python | ADBC manager | Outcome |
|---|---|---|---|
| macOS ARM64, native Redis 8.10.2/Search 8.10.1 | 3.13.16 | 1.8.0 | [15 passed](evidence/macos-py3.13-adbc1.8.0.json) |
| macOS ARM64, same owned fixture | 3.13.16 | 1.12.0 | [15 passed](evidence/macos-py3.13-adbc1.12.0.json) |
| macOS ARM64, same owned fixture | 3.14.8 | 1.8.0 | [15 passed](evidence/macos-py3.14-adbc1.8.0.json) |
| macOS ARM64, same owned fixture | 3.14.8 | 1.12.0 | [15 passed](evidence/macos-py3.14-adbc1.12.0.json) |
| Linux ARM64/AlmaLinux 8 container, Redis 8.6.2/Search 8.6.0 | 3.13.16 | 1.8.0 | [15 passed](evidence/linux-arm64-py3.13-adbc1.8.0.json) |
| Linux ARM64, same owned fixture | 3.13.16 | 1.12.0 | [15 passed](evidence/linux-arm64-py3.13-adbc1.12.0.json) |
| Linux ARM64, same owned fixture | 3.14.8 | 1.8.0 | [15 passed](evidence/linux-arm64-py3.14-adbc1.8.0.json) |
| Linux ARM64, same owned fixture | 3.14.8 | 1.12.0 | [15 passed](evidence/linux-arm64-py3.14-adbc1.12.0.json) |

**Eight local cells, 120 checks passed.** Linux ran on the native ARM64 Docker
VM using an AlmaLinux 8 client container and a shared network namespace with
its owned TLS Redis container. This is not a local x86_64 result or a hosted
Ubuntu result. The four x86_64 cells and the hosted 12-cell workflow remain
unverified until the new PR runs.

All cells preserved exact Arrow values/schema, credential-free descriptors,
closed-planner independent workers, Search and direct HASH reads, bad-CA and
wrong-host rejection, restricted permissions, malformed/stale descriptors,
cancellation, early release, live updates/deletes and later-insert exclusion.
The server's real aggregate cap was 137. No assertions or skips were relaxed.

## Commands and verification

macOS built with `GOTOOLCHAIN=go1.26.6`, `MACOSX_DEPLOYMENT_TARGET=12.0`, matching
cgo deployment flags and `make -B -C go build`. Linux used
`go/build-linux.sh arm64 v0.0.0-dev` with the cached Go 1.26.6 AlmaLinux 8 builder;
the symbol check reported highest GLIBC requirement 2.17. Both wheels used
version `0.0.0.dev0` and their release platform tags.

For each selected pair, equivalent commands were:

```sh
uv venv --python "$CLIENT_PYTHON" "$CLIENT_VENV"
uv pip install --python "$CLIENT_VENV/bin/python" "$RELEASE_WHEEL" \
  "adbc-driver-manager==$ADBC_MANAGER" pyarrow redis
# REDIS_TLS_ADMIN_URI and SSL_CERT_FILE point to the private owned fixture.
env -u REDIS_ADBC_LIBRARY "$CLIENT_VENV/bin/python" \
  scripts/check_partition_tls.py --require-wheel --output "$FRESH_REPORT"
```

`actionlint -shellcheck='' .github/workflows/release.yml` and whitespace checks
passed. An isolated YAML-parser check verified 12 unique combinations, reuse
of parent artifacts, the Go-change trigger, release publication dependency and
package-only release downloads. No driver runtime or checker code changed, so
full Go/upstream ADBC suites were left to the existing parent jobs.

## Cleanup and remaining gaps

The owned native macOS Redis PID was checked and stopped. The exact owned Linux
client container and label-verified TLS Redis container were removed. Both private
TLS fixture directories were removed. JSON evidence contains no credentials or
private keys. No paid service resources were created. GitHub runner minutes for
the added matrix belong to the repository; native libraries are built once per
platform, not once per client combination.

This adds a durable gate, not proof of every supported client/OS combination.
Python 3.10-3.12, the full upstream suite for every client version, the complete
Cloud matrix, failure/resharding behavior and representative performance remain
outside this change. Issue #363 stays open. Prior Cloud topology evidence is in
[#413](../cloud-partition-routing-363/evidence.md).

Agent review followed REVIEW.md for lifecycle, ownership/TLS, publication
permissions and evidence scope. No actionable findings remain from that review;
human review and hosted checks remain pending.
