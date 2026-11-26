# Validation

2026-11-26 (UTC). Base: main `a609aab0`. See [intent](intent.md), [spec](spec.md), [plan](plan.md).

**Result.**
- **Both pools fixed:** the parallel aggregate top-k and the parallel streamed star/snowflake chunk readers now report a statement deadline as `TIMEOUT` and a cancel as `CANCELLED`, each with SQLSTATE 57014, not the bare context error.
- **The failing case** is a unit test that reproduces Q75's exact error before the fix.
- **On SF1 Flex,** no run of any build reported `UNKNOWN`. Whether a deadline fires inside one of these pools depends on timing and cache state, so the live runs below do not reproduce #696; they show the change keeps every observed deadline at `TIMEOUT … 57014`.

## Unit tests (no Redis)

`pool_err_test.go`, `cd go && env -u REDIS_URI go test -count=1 -run TestPoolErr .`:

| Case | Behavior the pools had before | With the change |
|---|---|---|
| Statement deadline passed | `context deadline exceeded`: not an `adbc.Error`, no SQLSTATE (the Q75 symptom) | `TIMEOUT … the query timed out`, 57014 |
| Statement cancelled | `context canceled`, unwrapped | `CANCELLED … the query was cancelled`, 57014 |
| Worker error first, statement ends later | worker's error | worker's error |
| Worker cancels the pool with `context.Canceled`, then the statement's deadline passes | — | `TIMEOUT`, 57014; without the statement check it was `CANCELLED` (shown by removing it) |
| `errStarProbeStopped` sentinel | kept | kept |
| `closeWith` cause | kept | kept |

- **The "before" column:** the first run used a `poolErr` that returned `context.Cause(pool)`, the top-k pool's inline code. The deadline and cancel cases failed, as shown. The worker-error case's first failure was a test bug: `adbc.Error` is not comparable, so `errors.Is` cannot match it. It now compares status and message.
- **Unit-only suite:** `cd go && env -u REDIS_URI -u REDIS_FLEX_URI go test -count=1 -p 1 ./...` passed all 7 packages that have tests; exit 0.
- **Formatting and vet:** `gofmt -l` on the changed files reports nothing. `go vet .` reports only an existing warning in `live_ingest_creation_integration_test.go`, which this change does not touch.

## TPC-DS SF1 on Software Flex

**Fixture:** a dedicated one from `go/flex-setup.sh`:
- Redis 8.8.0 Flex (bigstore v2, Search on Flex), 1 shard, 8 GiB RAM tier of 64 GiB, `noeviction`, TLS, AOF every second, Search disk settings 8/2/3 MiB.
- Active 4x QPF (6 Search workers, `mnp`, 32 connections), verified through the management API before and after each run.
- TPC-DS SF1 (19,557,375 rows) loaded as managed tables and count-validated, and gated for index compaction as in the [SF1 record](../sf1-flex-suites/validation.md).

**Client:** 4-CPU / 8 GiB Linux client in the server's network namespace (macOS Docker Desktop host), `adbc.redis.flex=true`.

**Libraries:**
- main `7c83c486`: SHA-256 `717b20d9…`.
- This change: SHA-256 `2a61f78a…`.
- An interim build of this change (`727b51e2…`), in which `poolErr` checked only the pool's context.

| Run | Library | Result |
|---|---|---|
| Q75 alone, harness `--query 75` | main | `TIMEOUT: failed to read rows … 57014`, 61.1 s; deadline in the eager join (`lookup_join_order`), not a pool |
| Q75 alone, harness `--query 75` | interim | the same, 60.9 s |
| Q18 and Q4, deadlines 2–30 s (20 runs), then Q18 at 1–3 s ×4 (20 runs) | main | 40 × `TIMEOUT … 57014` |
| Q18 and Q4, deadlines 2–30 s (20 runs), then Q18 at 1–3 s ×4 (20 runs) | interim | 39 × `TIMEOUT … 57014`, 1 × `CANCELLED … 57014` (Q18 at 2 s); Q18 also completed at 12–30 s, its rows by then in the RAM tier |
| Q18, Q4 and Q75, deadlines 1–8 s ×3 (54 runs) | this change | 54 × `TIMEOUT … 57014` |

The interim build's one `CANCELLED` was not reproduced in 40 further short-deadline runs on either library, and its path was not identified. The final `poolErr` checks the statement's context before the pool's, as the old snowflake code did, so a deadline that has passed is a timeout even when a worker cancelled the pool first.

## Not covered

- **No integration test** forces a deadline inside either pool. Neither pool has a test hook, and this change adds none.
- **Other raw context-error paths,** and the generated C bridge's `UNKNOWN` fallback: see the [spec](spec.md#not-covered).
- **Q75's cost:** [#710](https://github.com/redis/redis-adbc-driver/issues/710).
- **Review:** human review is pending.
