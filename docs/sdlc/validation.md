# Verification and evidence

Snapshot: 2026-10-20, `6f42461`. Commands below are extracted from the Makefile,
tests and workflows; their inclusion does not mean they ran during extraction.
Use a disposable Redis database: fixtures write data and connections can recover
abandoned moves. Never target an application database for convenience.

## Local checks (repository root)

| Check | Command | Success and limits |
|---|---|---|
| Shared library | `make -C go build` | Exit 0; host library under `go/build/`; cgo/C compiler required |
| CLI | `make -C go adbc-cli` | Exit 0; `go/build/redis-adbc` |
| Unit pass without Redis | `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...` | Exit 0; Redis integration tests skip |
| Start default Software | `make -C go redis-up` | Disposable RAM database with Search; Docker Compose/curl/jq required |
| Go including integration | `REDIS_URI=redis://localhost:6379/0 make -C go test` | Exit 0; inspect skips, retain `-count=1 -p 1` |
| ADBC validation | `REDIS_URI=redis://localhost:6379/0 make -C go validate` | Exit 0, no failures; uv and Python >=3.13 |
| pgdiff harness | `make -C go pgdiff-test` | Exit 0; no compatibility claim from harness tests alone |
| PostgreSQL comparison | `REDIS_URI=redis://localhost:6379/0 make -C go pgdiff` | Inspect report and underlying runner status; known dialect differences are expected |
| Controlled gate tooling | `python3 -m unittest discover -s benchmarks/worker-autotune/controlled -v` | Exit 0; tests measurement tools, not tuner readiness |

For catalog changes, CI also runs `uv run --with 'sqlalchemy>=2.1' python
check_catalog_reflection.py` from `go/validation`, with `REDIS_ADBC_LIBRARY` pointing
to the built library. Format changed Go files with `gofmt`; no `make lint` exists.

## Choose checks by change

- SQL/types/catalogs: focused Go and Python cases, supported-environment tests,
  reflection when relevant, and pgdiff cases for PostgreSQL behavior.
- Storage/adoption/rekey: ownership, stale-writer, interrupted recovery and
  concurrent rename/drop tests, including cluster keys and CLI integration.
- Streams/binding/cancellation: early close, fetch failure, retained-buffer
  lifetime and token/cursor cleanup; validation stream/cancel cases.
- Worker policy: budget/tuner unit and race checks from the
  [readiness workflow](../../.github/workflows/worker-autotune.yml), plus the
  controlled matrix if making readiness or regression claims.
- Benchmark tools: corresponding Python helper tests. The main workflow runs
  ClickBench discovery with uv-provided pyarrow/redis/adbc-driver-manager/duckdb,
  autotuning tool tests, and QPF/worker-script syntax checks.
- Markdown only: local links/anchors, cited evidence and diff checks. Broaden
  testing only if commands, configuration or behavior also change.

## CI facts

[Test](../../.github/workflows/test.yml) runs on PRs matching its path filters:
driver files, selected benchmark directories and that workflow. Root guide-only
edits do not match those filters. Its eight jobs combine amd64/arm64 with
Software single/three-shard and OSS single/three-shard environments, all with
Search, and include Go, Python and reflection checks. A configured job is not
evidence that its latest run passed or is required by branch protection.

[pgdiff](../../.github/workflows/pgdiff.yml) and
[worker readiness](../../.github/workflows/worker-autotune.yml) are manual
`workflow_dispatch` jobs. The pgdiff runner returns 0 for matching scored cases,
1 for compatibility differences and 2 for infrastructure/fixture failures. Make
can wrap a failed recipe with its own status; inspect the runner report.
The workflow intentionally reports status 1 without failing its job, and fails
on infrastructure errors. Inspect `go/build/pgdiff-report.json`, not just green CI.

Software blocks protocol ACL administration and some resettable counters;
OSS checks cover those cases. Pause tests require `REDIS_PAUSE_TESTS=1` and
disrupt a test server. Environment-specific skips need explicit accounting.

## Evidence to attach to each PR

Record source commit, changed contract, exact commands, exit results, relevant
counts/skips, Redis/Search versions, topology, architecture and artifact links.
For a bug, retain the failing repro and the passing result after the fix.
Never silently relax expectations or relabel unresolved SQL failures as supported.

For performance, retain fixture seed/size, query text/hash, driver revision,
CPU/memory limits, QPF, worker ceilings/actual grants, arrival pattern, warmups,
repetitions and output validation. Include execute plus complete fetch, latency,
throughput and client resources. Keep incomplete/killed runs and identify them.
See [historical data](context.md#historical-data-with-limits).

## Future agent evaluations

No agent-eval CI was found. A proposed suite should contain representative bug,
SQL, lifecycle, benchmark and documentation tasks, each with a frozen starting
revision and externally specified checks. Evaluate guide/skill/hook changes
against those outcomes and review any reduced pass rate. Do not report the
product test suite's success rate as an agent-evaluation score.
