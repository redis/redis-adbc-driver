# Verification and review

Date: 2026-10-22. Status: local verification complete; PR review pending.

- `bash -n go/software-setup.sh go/software-qpf.sh go/flex-setup.sh`: passed.
- QPF gate regressions: 3 test methods passed, including subcases for null or incorrect workers, wrong routing, connection count, lifecycle state and QPF factor. Standard fixtures are upgraded; correctly configured fixtures require no reconfiguration.
- Software TLS management-safety regressions: 3 passed.
- CI matrix regressions: 5 passed; fixture changes select the full architecture/topology matrix.
- Live pinned Redis Software 8.2.0-78.18 (Redis 8.6.2): single-shard creation and reuse verified; a fresh three-shard fixture verified. Both TLS topologies passed certificate-verified proxy access, plaintext refusal, and effective QPF readback. Each reported active factor 4, six Search workers, mnp routing, and 32 connections; the partition-test aggregate cap remained 137.
- The management API only expands `search` when requested with `fields`; readiness checks explicitly request it rather than treating the default BDB response as an effective worker readback.
- `git diff --check`: passed. Review found no driver hot-path, ownership, credential handling or provider-matrix expansion. Private benchmark records, credentials, license files and infrastructure identifiers are excluded from this change.

Limits: the Flex startup path shares the tested gate but was not live-rerun. The full Go/ADBC suite was not rerun locally because this change affects fixture readiness rather than driver logic; hosted CI remains pending. The full-scale Software ClickBench restart requires a licensed deployment and a verified compatible backup and is not claimed as validation of this PR. No speedup is claimed from setting QPF.

See [intent](intent.md), [specification](spec.md), and [plan](plan.md).
