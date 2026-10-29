# Specification: installation diagnostics and setup

Date: 2026-10-29. Source: `95d6583` plus this change. Status: implemented locally.
Inputs: [intent](intent.md). Execution: [plan](plan.md).

## Functional requirements

- `python -m adbc_driver_redis doctor` loads the native library, checks its ADBC
  entrypoint and DB-API dependencies, and reports package metadata/platform hints.
- A URI from `--uri` or `REDIS_URI` enables a normal connection and fully fetched,
  validated `SELECT 1`. `--offline` overrides either URI source. No URI means
  offline checks; skipped checks are explicit.
- `--check-manifests` requires an online URI and checks both driver names against
  the supplied endpoint. TLS is exercised only with a TLS endpoint URI.
- Return 0 for successful requested checks, 1 for diagnostic failure and 2 for
  invalid arguments. Suppress raw library/driver errors and URI values. Errors
  get fixed, actionable authentication, ACL, TLS, Search, Flex, timeout or generic
  connection hints. Preserve install-manifest options and behavior.
- Online diagnostics use normal driver metadata setup/recovery and five-second
  per-command read/write limits, not a global wall-clock deadline. They perform
  no explicit table DDL or ingestion. Connection/cursor cleanup must occur even
  when results are invalid.
- Publishing validates the existing .so wheel member as Linux ELF64 x86-64/ARM64
  or macOS Mach-O ARM64 according to the supported wheel filename. Retain the
  existing exact platform-set, checksum and package metadata gates.
- Setup documentation links all 22 ecosystem tools to an implemented or candidate
  path, distinguishes tests from claims of successful runs, and documents known
  platform, TLS, manifest-lifetime and autocommit limits.

## Risks and evidence

Do not imply plaintext checks test TLS or that all ADBC integrations are complete.
Manifest-name checks can load another installation and are not a binary identity
check against the Python wheel. Header validation checks format/architecture, not
full loadability or provenance; existing release load tests and checksums remain
necessary. See [validation](validation.md) for actual runs and limitations.
