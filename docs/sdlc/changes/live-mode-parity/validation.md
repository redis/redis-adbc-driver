# Combined feature validation

Date: 2026-11-20. Author: Codex. Requestor: Albert Wong.
Base: current main `845c95b4`. Status: verified locally; human review pending.

Integrated #553 (`45ad2dcd`), #554 (`f3191924`) and #555 (`93c6ce96`) onto
current main in an isolated worktree. Resolved overlapping documentation to
retain MERGE, incremental streaming and explicit evolution together. Preserved
the newer statement-operation timeout in live write dispatch. Added a source-build
availability note, evolution permission requirements and explicit zero-byte
fixture assertions. Unrelated existing workspace changes were not included.

## Checks and results

The retained dedicated local fixture was initially created with `make -C go
redis-up`. Each integration invocation ran through its owning worktree's
`software-setup.sh --exec` gate. It reverified Software 8.8.0, three active primary
shards, RAM/noeviction, TLS and active 4x QPF (six Search workers, mnp routing,
32 connections). Private Compose overrides, endpoint/trust configuration and
raw test logs remain outside version control.

From the integrated `go/` tree, after the gate exported the verified endpoint and
`REDIS_SNAPSHOT_URI` was set to that same endpoint:

```sh
go test -json -count=1 -p 1 -timeout=15m \
  -run '^(TestLive|TestStream|TestSQLMerge|TestSQLReturningMerge|TestScanProjection|TestUnmanagedProjection|Test.*SourceKey|TestStatement.*Timeout)' .
```

Exit 0: 75 top-level tests and 105 subtests passed (180 pass events); three
opt-in unmanaged profiling tests skipped. No failures. This includes the new
`TestLiveModeParityEvolutionStreamAndMerge`: an open live stream rejects its old
definition after explicit schema/index replacement, then new SQL and MERGE use
the replacement while preserving the removed SQL column's source field and TTL.
The selection also exercises managed MERGE, source-key provenance, existing live
SQL/Arrow writes, stream lifecycles and projection regressions.

After adding explicit zero-byte fixture assertions, a focused race run used the
same verified gate:

```sh
go test -race -json -count=1 -p 1 -timeout=10m \
  -run '^(TestLiveModeParityEvolutionStreamAndMerge|TestLiveEvolutionSourcePreservation|TestLiveCollectionIncrementalStream)$' .
```

Exit 0: three top-level tests and five subtests passed; zero skips, failures or
race reports. This checks the final integration test and updated binary fixtures.

- `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -json -count=1 -p 1 ./...`:
  exit 0 across all packages with tests; 1,829 pass events and 1,514 expected
  skip events, including integration tests without a configured endpoint.
  These skips establish no additional server compatibility.
- `make -C go build adbc-cli`: exit 0; shared ADBC library and CLI built using
  repository version flags.
- `gofmt`, `git diff --check`, local documentation link-target checks: passed.

Individual feature records contain the additional focused/fault-injection
evidence. All three task-owned Redis Software fixtures were stopped after
validation, retaining their private restart configuration. Other tasks' running
instances were not changed.

## Review and limits

Codex reviewed the combined code and documentation against REVIEW.md. Subagents
independently reviewed streaming, evolution and MERGE respectively, with no
remaining actionable findings. Review covered source ownership, exact values and
NULLs, binary keys, complete predicates, schema/index identities, uncertain
outcomes without mutation replay, partial-write errors, cancellation and Arrow
reader cleanup. This is agent review, not human approval.

No full server suite, independent native C execution, Cluster API, real restricted
ACL principal, failover, Flex or performance run was performed for the combined
change. Live registrations remain excluded from Flex; SQL ALTER/CREATE INDEX/DROP
INDEX remain unsupported for live tables. Evolution is an API/CLI workflow with
explicit writer quiescence; retained live results remain tracked by #550.

See [intent](intent.md), [spec](spec.md) and [plan](plan.md).
