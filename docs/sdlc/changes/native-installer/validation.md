# Validation: native installer and live diagnostics

Date: 2026-10-29. Base: `b1d0613` plus this change.
Inputs: [intent](intent.md), [spec](spec.md), [plan](plan.md).

## Local checks

- Built the current native library with `GOCACHE=/private/tmp/redis-adbc-active-go-cache
  make -C go build`: passed, macOS ARM64, version linker flag retained.
- Created a dedicated Compose project `redis-adbc-install-doctor`, API port 29443,
  database port 26379. Redis Software image `redislabs/redis:8.2.0-78.18`, Redis
  8.6.2, Search enabled. Verified effective QPF active at 4x, six Search workers,
  mnp routing and 32 connections before testing. The existing Software container
  owned by another chat was not changed.
- Complete focused suite, with the current source library and authenticated
  plaintext Software connection: **24 passed, no skips**. Command:
  `python -m pytest -q python/tests/test_doctor.py python/tests/test_installation.py
  python/tests/test_driver.py`, using `PYTHONPATH=python`, `REDIS_URI` and
  `REDIS_ADBC_LIBRARY` in the isolated test environment.
- `python3 -m unittest discover -s scripts -p test_install_driver.py -v`:
  **16 passed**. Includes upgrade, stable paths, original manifest restoration and
  permission modes, ownership conflicts, checksums, archive safety, locking,
  failed-install cleanup, failed-upgrade rollback and preserved recovery backups.
- `python3 -m unittest discover -s scripts -p test_check_wheel_libraries.py -v`:
  **2 passed**. Total focused Python/unit tests: **42 passed**.
- Software TLS helper generated private local trust material without changing
  global roots. `scripts/check_install_doctor.py` passed **seven live scenarios**:
  verified TLS plus both manifests, untrusted CA, bad authentication, denied
  FT._LIST, broken manifest, missing library, and missing Search. The missing
  Search endpoint was a separate `redis:8.6.2` container launched without Search,
  used only as a negative fixture. [Redacted results](live-doctor.json) contain no
  URIs, credentials, certificate keys or management response bodies.
- `scripts/check_native_install.py` with the real current macOS library passed
  install and upgrade using synthetic local release versions, then fully fetched
  `SELECT 1` via both manifests over verified Software TLS after each operation,
  and uninstalled cleanly. This checks the installer lifecycle, not publication of
  the synthetic version tags.
- Downloaded actual GitHub v0.0.18 macOS native assets with the new installer,
  verified their checksum and native loadability, reinstalled via upgrade, then
  uninstalled from temporary custom paths. No user-level driver was installed.
- Checked **91 local documentation link targets**, balanced code fences,
  Python 3.12 syntax and both changed workflow YAML files. `git diff --check`
  passed.

Environment: Python 3.15, adbc-driver-manager 1.13.0, PyArrow 26.0.0, Polars 2.1.0,
redis-py 8.1.0, pytest 9.2.0, in an isolated temporary virtual environment.
An initial broader test attempt found Polars absent in a reused environment;
that environment was not modified, and the complete suite above ran in the
new environment with all dependencies present.

## CI and limits

PR #240 is merged. Its Test matrix (all eight Software/OSS topology/architecture
jobs), helper checks, three native build jobs and Python wheel compatibility
checks passed. At the last inspected snapshot, its AlmaLinux 9 ARM64 distribution
validation job also succeeded, completing the distribution matrix. All reported
validation/build checks succeeded; the release upload job was skipped as expected
for a PR run. Evidence: [Test run](https://github.com/redis/redis-adbc-driver/actions/runs/40976474725)
and [Release binaries run](https://github.com/redis/redis-adbc-driver/actions/runs/40976474583).

This follow-up adds native lifecycle checks for Linux/macOS release builds and
the Software TLS diagnostic harness for Linux release builds. Those new hosted
checks are not claimed to have passed before they run. Local native lifecycle
evidence covers macOS ARM64, not Linux architectures. No release was published.
No human merge approval is recorded.

Cleanup completed: removed the dedicated Software project, missing-Search
container and generated private fixture certificates/keys. Confirmed that only
the pre-existing Software container remained. Temporary managed installations
were uninstalled; no user manifest or driver directory was changed.
