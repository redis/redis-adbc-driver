# Redis ADBC driver

Go implementation of SQL and Arrow bulk ingestion over Redis HASHes and Search,
built on driverbase-go. Read [the SDLC index](docs/sdlc/README.md) and the relevant
feature documentation before changing behavior. The module is in `go/`, not the
repository root; its toolchain is declared in `go/go.mod` (currently Go 1.26.6).

## Commands (from the repository root)

- Shared library: `make -C go build` (requires cgo and a C compiler; creates
  `go/build/libadbc_driver_redis.{so,dylib,dll}` for the host).
- HASH adoption CLI: `make -C go adbc-cli` (no cgo).
- Unit-only pass: `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...`;
  integration skips are expected and do not prove Redis compatibility.
- Disposable Redis Software with Search: `make -C go redis-up` (Docker Compose,
  curl, jq). Use a dedicated test database; suites modify data.
- Full Go suite: `REDIS_URI=redis://localhost:6379/0 make -C go test`.
- ADBC validation: `REDIS_URI=redis://localhost:6379/0 make -C go validate`
  (uv, Python >=3.13). Success is zero failures; report skips explicitly.
- Differential harness tests: `make -C go pgdiff-test`. Full comparison:
  `REDIS_URI=redis://localhost:6379/0 make -C go pgdiff` (Docker, uv).
- Format changed Go files with `gofmt`; there is no Makefile lint target.

## Architecture and invariants

- `go/pkg/` exposes the ADBC C boundary; `driver.go`, `database.go`,
  `connection.go`, `statement.go` implement handle lifecycle and options.
- `sql.go`, `query.go`, `eval.go` implement SQL; `store.go` owns the HASH/index
  layout. `ingest.go`, `arrowconv.go`, `stream.go` connect Arrow to execution.
- Row HASHes are authoritative. Index pushdown must preserve exact values,
  NULLs, ordering, precision and residual predicates; fall back when unsafe.
- Metadata uses `adbc:{meta}:` to share a cluster slot. Row prefixes are never
  reused. Preserve rename/rekey recovery and external HASH ownership rules.
- Autocommit only; Search is required; Flex is rejected. Do not imply full
  PostgreSQL compatibility or support for Valkey / Redis 7.2 derivatives.

## Verification and recurring pitfalls

Keep Go packages sequential (`-p 1`) against a shared Redis: opening a connection
can recover another package's abandoned move. Disable test caching (`-count=1`).
Software blocks some protocol ACL and command-counter checks; OSS covers them.
Do not weaken assertions or broaden skips to hide a regression. For a bug fix,
capture a focused failing case before the fix when practical, and retain it.
Run checks appropriate to the changed behavior and report commands, outcomes,
skips and limitations. Documentation-only edits need link/evidence checks.
Finish Arrow fetch and validate results before claiming query success or speedup.
Preserve cursor cleanup, cancellation, Arrow ownership and worker-token release.
Use [REVIEW.md](REVIEW.md); record change-specific intent/spec/plan and evidence as
described in [the workflow](docs/sdlc/README.md). Historical measurements are not
current validation. Markdown instructions are advisory, not installed hooks.
