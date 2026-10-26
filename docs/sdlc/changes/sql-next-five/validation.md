# Verification and review

2026-10-22. Base `d72b730`; author/reviewer Codex (self-review, not human approval).
[Intent](intent.md), [spec](spec.md), [plan](plan.md), [manifest](evidence/manifest.json).

## SQL behavior

- Original issue failures are retained in [baseline reproductions](evidence/baseline-sql-reproductions.json).
- All **35/35 differential cases** match PostgreSQL 16.10 on rows, Arrow-equivalent
  result types and SQLSTATEs: every #188/#241/#242/#244 corpus example plus 22
  additional cases. [Report](evidence/postgres-comparison.json), [extra corpus](evidence/edge-cases.json).
  Reference is the cached local PostgreSQL 16.10 image with C collation and UTF-8
  client encoding, not the harness default 16.15 digest. No full-conformance claim.
- Full sequential Redis Go suite: **1,244 tests/subtests passed**, zero failures,
  **20 skips**, all three packages passed. [Summary with every skip](evidence/go-suite-summary.json).
  Command: `REDIS_URI=redis://127.0.0.1:26389/0 GOCACHE=/private/tmp/redis-adbc-go-cache go test -C go -count=1 -p 1 -json ./...`.
- Subsequent focused tests cover the final VALUES category guard, prepared-statement
  rebinding, GetParameterSchema, schema-only planning without evaluating LIMIT,
  stored-view time-zone freezing, defaults/aliases and wide-buffer/error behavior.
  [Focused log](evidence/focused.log). No runtime error was turned into a skip.
- Focused race pass: new SQL planning and typed Arrow tests, existing Arrow ownership
  and HASH slice cancellation hooks. [Log](evidence/race.log). No Cluster race claim.
- Python native integrations: **16 passed**; isolated Superset compatibility: **3 passed**.
  [Python](evidence/python.log), [Superset](evidence/superset.log).
- Differential harness unit tests: **25 passed**; benchmark harness: **6 passed**.
  [pgdiff](evidence/pgdiff-unit.log), [benchmark harness](evidence/harness-tests.log).
- Native libraries built with `make -C go build LIB=<temporary-path>` and the
  normal version linker flag. gofmt, Python syntax and `git diff --check` pass.

## Typed Arrow gate (#446)

Same independently typed official ClickBench prefix, 100,000 rows x 105 columns,
79,578,019 logical Arrow bytes; three-column control selects watchid/title/eventtime.
Redis 8.6.2 / Search 8.6.0, Docker limit four CPUs / 2 GiB; native macOS arm64 Go
1.27.2. Both builds use RESP3 and 4,096-row batches. Regular API uses one reader;
public partitions use four readers over eight descriptors. One warmup and five
measured trials per build/API/shape, alternating order: **48 complete executions**,
40 measured. Every selected value and duplicate multiplicity independently checked
outside timing. [Wide raw trials](evidence/final-wide.json), [narrow raw trials](evidence/final-narrow.json).
Actual first batches are 4,096 rows in every final trial; final/minimum batch sizes
are retained per trial. No network, pipeline, eligibility or batch-size change.

| Shape/API | Baseline median (range), seconds | Candidate median (range), seconds |
|---|---:|---:|
| Wide regular | 3.467 (3.315-3.643) | 3.140 (3.067-3.193) |
| Wide partition | 1.801 (1.612-1.862) | 1.669 (1.618-1.784) |
| Narrow regular | 2.327 (2.272-2.440) | 2.342 (2.289-2.354) |
| Narrow partition | 0.958 (0.786-1.053) | 0.824 (0.789-0.968) |

Wide medians improve **9.4% / 7.3%**. Narrow regular differs by +0.7% with overlapping
ranges; no narrow throughput gain is claimed. Partition measurements include
process startup and show wider variance; do not infer universal scaling or a
production deployment speedup. Timings include startup/import, connection and
planning, complete fetch, safe Arrow normalization, IPC output and teardown.
Client/server CPU, request/reply bytes and worker lifetime peak RSS are retained
in raw trials. Request/reply work is essentially unchanged. Peak RSS is a worker
lifetime maximum, not precise concurrent process memory or Go live heap.

The earlier unrestricted prototype regressed the narrow partition control. It is
retained in [unguarded wide](evidence/unguarded-screen-wide.json) and
[unguarded narrow](evidence/unguarded-screen-narrow.json). Only projections with
at least **64 outputs** now specialize. The lower-level projection still validates
all needed fields in metadata order, including unselected fields and skipped rows;
output coercion still validates narrow integer widths. Unsupported encodings and
uncommon builders retain generic decode/coerce/append behavior. Temporal, decimal,
CHAR, binary and NULL conversion semantics retain the generic path where necessary.

Separate Go diagnostics execute three scans outside timed trials:

| Diagnostic | Baseline | Candidate |
|---|---:|---:|
| Cumulative allocated bytes | 3,479,293,960 | 3,479,541,904 |
| Allocations | 99,109,736 | 99,111,805 |
| GCs | 98 | 94 |

Allocation is essentially unchanged; this is a dispatch/CPU optimization.
[Baseline log](evidence/baseline-profile.log), [candidate log](evidence/candidate-profile.log),
[baseline CPU](evidence/baseline-cpu.txt), [candidate CPU](evidence/candidate-cpu.txt),
[candidate allocations](evidence/candidate-alloc.txt). Raw CPU/allocation pprof
files are retained alongside them. Go-allocator diagnostics differ from native
C-allocator timed workers; sampled CPU shares are inclusive and not wall-time predictions.

## Review and limits

Correctness/lifecycle review caught and corrected: INSERT VALUES accidentally using
query-level common-type conversion, cached GROUP BY slice mutation after planning
copies, BOOLEAN/numeric VALUES mixing, and local-time VALUES literals in stored
views. Existing repeated-alias, keyset and window-error regressions pass. Old
unsupported no-FROM aggregate assertions became positive result assertions;
unknown-column/type error checks remain. Runtime counts live only in plan copies;
static expressions are cleared after resolution so replanning a DISTINCT/group
optimization cannot repeat side effects. Schema-only planning never evaluates counts.

Security/ownership review checks retained-query expression admission for both new
clauses, existing result allocation budgets, cancelled VALUES evaluation, unchanged
managed-HASH authority and network/cursor ownership. Arrow tests cover exact BIGINT,
width overflow, defaults/missing markers, aliases/duplicate outputs, byte budgets,
required-field error ordering and retained batches after release. No Redis command,
ACL/TLS, storage layout or concurrency change is introduced.

Not rerun: external dbt functional suite, full ADBC validation suite (its package is
absent from the available runtime), Redis Software, Cluster/TLS deployment matrix,
or Linux wheels. No Cloud resources were created. Three task-owned local containers
are removed after verification. This agent review does not replace maintainer review.
