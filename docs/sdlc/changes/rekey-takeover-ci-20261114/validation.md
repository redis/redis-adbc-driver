# Validation and review

Date: November14,2026 (America/Los_Angeles). Author/reviewer: Codex.
[Intent](intent.md), [spec](spec.md), [plan](plan.md). Human review pending.

- Original Actions evidence: https://github.com/redis/redis-adbc-driver/actions/runs/43564885518/job/132356530814
  TestSQLRekeyTakeoverWait failed with directory344 instead of600, exactly one
 256-row deletion before the controlled barrier. Other latest main Test and
 DuckDB workflows passed; PR437 was already merged.
- Baseline focused test30 repetitions passed locally; this does not reproduce
  the intermittent macOS schedule. CI evidence and source show the independent
  background collector can win before the explicit one.
- Corrected focused test50 repetitions with race detection: PASS (43.02s), zero
  skips; original assertions retained. Command: go test -race -count=50 -p1
  -timeout=10m -run '^TestSQLRekeyTakeoverWait$' .
- Local owned Redis Software fixture configured with make -C go redis-up and
  independently checked before runs: active/effective4xQPF, six Search workers,
  mnp32, one4-GiB noeviction shard, actual4-CPU/8-GiB container, strict dataTLS
  and RESP3. No Community/alternative-server local test substituted. Clients
  exited before the fixture was leased serially to the next authorized performance
  subagent; root coordinates final ownership cleanup after all tasks.
- CI matrix selector: nine tests PASS. Distribution helper checks PASS.
  git diff --check PASS. No production code or historical benchmark changed.

Review: explicit stop joins the only unintended collector before expiry. The
second driver connection still runs normal recovery while the paused first
collector is held, and all stale-batch/name-reuse assertions remain. No sleep,
retry or broadened skip masks the failure. The workflow filter adds coverage.
Operational logs, endpoints, generated credentials and infrastructure identities
remain private. Remote workflow validation is pending until the PR runs.
