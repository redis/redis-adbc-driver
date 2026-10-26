# Plan and evidence

2026-10-21; baseline 9f5188c; #363; see [intent](intent.md), [spec](spec.md).

1. Add a disposable TLS fixture with fresh certificates and cleanup guidance.
2. Add native cross-process partition validation with explicit ACL/cap contracts,
   certificate negative checks, exact values/schema and bounded lifecycle checks.
3. Require it in existing OSS-single PR jobs and installed-wheel release tests.
4. Validate on a Linux arm64 native library/wheel locally, check shell/Python/YAML
   and required negative cases, record remaining platform/deployment gaps.
5. Open a separate PR referencing #363, which remains open for broader coverage.

The driver uses Go's ordinary certificate verification; Linux supports a task-
local SSL_CERT_FILE trust root. Do not weaken it with skip_verify or install a
fixture CA into the host keychain. No paid infrastructure is needed.
## Verification

- Built main's native Linux arm64 library in the cached Go 1.26.6 AlmaLinux 8
  release builder; highest required GLIBC symbol was 2.17 (within the 2.28 floor).
- Built/installed the manylinux_2_28_aarch64 wheel with the release packager.
  Linux Python 3.13.16, ADBC manager 1.12.0, PyArrow 26.0.0, redis-py 8.1.0.
- Both built-library and installed-wheel entry paths passed all 14 native TLS
  checks, including explicit untrusted-CA rejection in a fresh Go worker and
  hostname mismatch rejection under the verified CA policy. Complete source
  Arrow values/schema matched on 1099 broad rows and selected/deleted IN keys.
- Search-backed reads with a real cap 137, direct HASH reads, independent ACL
  denials, closed planner, cancellation, early release, live updates/deletes,
  exclusion of later inserts and stale descriptors passed.
- [Built library report](evidence/linux-arm64-library.json) and
  [installed wheel report](evidence/linux-arm64-wheel.json) contain versions,
  library hashes and per-check results, without credentials/certificate keys.
  Fresh workers receive only their own principal via stdin and a minimal
  runtime/trust environment, never planner/admin connection variables.
- Shell syntax, Python compile, actionlint 1.7.12 (Test and Release workflows;
  shellcheck disabled because unavailable), and diff checks passed.
- Initial local fixture certificates lacked strict X.509 CA/key identifier
  extensions. Python rejected them; fixed certificate generation without
  bypassing verification. Python installation in a macOS case-insensitive
  bind mount also failed on terminfo filename collisions; keep the disposable
  Linux interpreter on the container's internal filesystem instead.
- All 11 checks on the prior merged runtime PR #388 passed. This change adds
  validation/workflow/docs only; no Go runtime implementation changed, so the
  new native TLS checks are the relevant additional runtime verification.

## Remaining limits

Local success is Linux arm64, standalone Redis 8.6.2/Search 8.6.0. The new amd64
and release workflow jobs have not yet run for this PR. CI definitions alone
are not success evidence. Cloud/Software proxy TLS, packaged macOS TLS and
remaining deployment/client-version coverage stay open in #363. Nothing here
implies snapshots or snapshot-compatible retries. No paid resources provisioned.
Fixture schemas/users and only task-owned containers/certificates are cleaned
up; no CA is added to a host/system trust store.

## Tracker reconciliation

#358, #361 and #372 were implemented by merged PR #373, which referenced
rather than closed them. Rechecked the current tests and historical cap-change
evidence, linked that evidence in the checklists, and closed these completed
implementation issues. #359/#360/#362 remain designs; #363 remains open for
broader platform/deployment validation.

## Hosted follow-up evidence (2026-10-21)

The installed-wheel TLS jobs in merged PR #389 subsequently passed on both
Linux amd64 and arm64 in [release run 39652291906](https://github.com/alberttwong/redis-adbc-driver/actions/runs/39652291906).
Both uploaded reports record all 14 checks passed with certificate verification,
actual cap 137 and independent restricted workers. Python 3.12.3, PyArrow 26.0.0,
ADBC manager 1.12.0 and redis-py 8.1.0. See
[amd64 report](evidence/ci-linux-amd64-wheel.json) and
[arm64 report](evidence/ci-linux-arm64-wheel.json). This supersedes the earlier
pending hosted-Linux gate; Cloud/proxy and macOS TLS remain open in #363.
