Target: redis/redis-adbc-driver — new issue
Title: Bump go-redis to v9.24.0 for the Tx.Close WATCH/MULTI cleanup fix (go-redis#4315) and re-qualify
Labels: bug
---
## Problem

go-redis v9.23.0 can return a pooled connection that still has WATCH or MULTI armed after a cleanup failure. We reported this as https://github.com/redis/go-redis/issues/4315. It was fixed by https://github.com/redis/go-redis/pull/4316 (merged 2026-11-16), which is released in v9.24.0 (2026-11-24). We still pin v9.23.0 (`go/go.mod:11`).

We use the affected path heavily:
- `Watch` at `go/store.go:933`, `go/store.go:1294`, `go/store.go:1450` and `go/store.go:1665`, with `TxPipelined` at `:957`, `:1298`, `:1483` and `:1673`.
- `go/tags.go:460` and `:481`.
- `go/adopt.go:464` and `:502`.

There is no local workaround (no UNWATCH or connection-retirement handling outside tests), so nothing needs removing.

## Proposed change

1. Bump `github.com/redis/go-redis/v9` to v9.24.0 and run `go mod tidy`.
2. Re-qualify the v9.24.0 behaviour changes that touch our usage:
   - `MaxActiveConns` now counts dedicated and excess connections plus in-flight dials, so bursts can return `ErrPoolExhausted`. We pass it at `go/database.go:252`.
   - `WithTimeout` clients now keep their parent's hooks (`go/aggregate_compact_integer.go:53`).
   - Cluster topology loading now prefers `CLUSTER SHARDS`. Check against the Software Cluster API fixture.
   - `TxPipelined` now drops the connection on an `EXEC` result-count mismatch or a rejected `MULTI` (redis/go-redis#4186, redis/go-redis#4192).
3. If a deterministic peer is practical, add a regression test for the two redis/go-redis#4315 cases:
   - a callback error followed by a refused UNWATCH;
   - a refused EXEC while in MULTI.
   In neither case may a later borrower inherit transaction state.
4. Update `docs/upstream-submissions/performance-2026-11-14.md:6`, which still lists redis/go-redis#4315 as an open request. New text: "fixed in v9.24.0 via redis/go-redis#4316; adopted in <PR>".

Historical benchmark records that name v9.23.0 describe past runs; leave them alone (e.g. `benchmarks/upstream-allocation/README.md:11`, `benchmarks/typed-hmget/2026-11-13/report.md:18`).

## Acceptance

- `go test -count=1 -p 1 ./...` and the live Software suite (`make -C go test-software`) pass with v9.24.0 on a verified fixture.
- The `Watch`-based transaction and lease integration tests pass, including the cancellation and refusal paths.
- Docs updated as above.
