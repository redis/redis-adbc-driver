# Unmanaged selected-field retrieval — November 14, 2026

Related implementation: [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).

Native HMGET removes the guarded per-row Lua path while preserving live source
semantics. In the matched **Go ADBC SQL diagnostic**, pooled p50 improves
**99.064→77.224 ms (1.28×)** with one client and **176.492→98.245 ms (1.80×)**
with four. Both actual SQL and complete typed Arrow results were validated in
all **32 ABBA observations / 1,024 queries**, including 256 warmup queries. All
768 measured observations remain; there are 192 per source/client combination.
These are not the Python/C-boundary native scenario matrix or capacity results.

## Why change the guarded reader

The original native benchmark recorded a material four-client filtered COUNT/SUM
regression. A separate retrieval-only profile on the same 100K four-column plus
ordinal shape compared actual guarded selected-field reads with original native
HGETALL. Its complete [16 observations](retrieval.json) reproduce the shape:
guarded reads are faster with one client but slower with four. The source was
`55f3d91f`, including a digest-budget prototype that does not change the short-key
fixture's 1,000-command batching. That prototype is superseded, not an explanation
for the standard regression. Request fanout remains 10,000 row reads/query.

The replacement uses native HMGET. Any non-NULL field, including an empty string
or binary/NUL value, proves the HASH existed at that atomic native observation.
An all-NULL vector instead uses original HGETALL and returns only that complete
later live image, never a mixture. Missing/non-HASH keys remain omitted, and SQL
rechecks the existing residual. Unmanaged tables do not promise a statement
snapshot. Zero-field and oversized requests retain native HGETALL. Native reads
avoid TYPE/HSTRLEN, Lua ACL preflights, script-cache repair and Lua value copies.
No application source or index is modified; no native client wrapper is required.

Every command carries NoRetry. The whole outer pipeline and every reply are
checked before any all-NULL or exact native HMGET command/key-permission fallback.
Only direct native WRONGTYPE is omission. Whole-command Nil, malformed vectors,
wrapped/joined refusals and unknown transport outcomes fail without replay.
Requests retain 256-field/1,000-command/1 MiB targets; native source values are
never truncated or rejected merely because they exceed the retired Lua limit.

## Matched full SQL observations

Fixture: 100,000 live source HASHes, four public int64/string columns plus the
ordinal control field, with the same non-SORTABLE value attributes and SORTABLE
ordinal predicate as the native scenario. Each query matches 10,000 rows and
returns BIGINT COUNT/SUM, completely fetched and checked against independent
arithmetic. Each trial performs 32 queries; one/four persistent ADBC clients use
a workerless protocol fixture with Go 1.27.3 and GOMAXPROCS=4. Statement planning,
execution and complete typed Arrow consumption are measured. Setup, connection
creation and the arithmetic oracle are outside per-operation samples. Hook
instrumentation and Go execution differ from the native matrix.

Both non-race diagnostic binaries completed compilation before any timing. Order
was guarded A1, native B1, native B2, guarded A2. Each pass retains one warmup and
three measured trials at both client counts, using identical test source. Exact
per-operation nanoseconds, allocations, command counts, wait times and wall
seconds are in [sql.json](sql.json); exact pooled statistics in
[comparison.json](comparison.json).

| Clients | Guarded p50 ms | Native p50 ms | Guarded p95 ms | Native p95 ms | p50 speedup | Guarded/native median finite queries/s |
|---:|---:|---:|---:|---:|---:|---:|
| 1 | 99.064 | 77.224 | 115.454 | 92.171 | 1.28× | 10.032 / 12.699 |
| 4 | 176.492 | 98.245 | 206.532 | 129.343 | 1.80× | 22.325 / 40.980 |

Median allocated bytes per 32-query trial decrease about 5.6%:
683.502→644.891 MB with one client and 685.325→646.586 MB with four (decimal MB).
Source fetches change from 320,000 EVALSHA_RO commands/trial to 320,000 native
HMGET commands/trial, in addition to unchanged metadata reads. Search remains
32 aggregates and 64 cursor operations/trial. Fewer row commands are not claimed.

Endpoint INFO CPU fields stayed zero and some endpoint network counters were
cached/unchanged. They are **unavailable attribution**, not evidence of zero CPU
or exact bytes saved. Extra Lua/ACL/length-check work was a plausible contention
hypothesis; this experiment establishes measured behavior of the replacement,
not a measured server-CPU cause. No server CPU percentage or exact network-byte
claim is made. Command waiting includes client pool/decoding and hook overhead.
ABBA reduces order drift but is not randomized evidence or a confidence interval.

## Sources, checks and limitations

[Provenance](provenance.json) records actual source and test-binary/harness hashes,
including unchanged post-run binaries. Guarded baseline production is PR #439
source `7b94a93c`, with only the matched test harness added in `164db2c2`.
Native diagnostic source is `859a9510`. A later source-only compatibility follow-up
recognizes exact native key-permission refusals for ACL selectors as well as
command denials, with HGETALL still doing its own authorization. That cold-path
follow-up is not relabeled as measured or checked by this diagnostic; parent
final checks/matrix must use the integrated final source. Frozen binaries remain
unchanged.
Frozen [source/harness copies](source/) make those exact inputs inspectable
without relabeling them as a later integrated parent build. All live commands
used the coordinated Redis Software fixture with independently verified active/
effective 4× QPF, six workers, mnp/32, one 4-GiB noeviction RAM shard and verified
certificate/hostname data TLS with RESP3. Actual [environment](environment.json): Redis Software 8.2.6-25 / Redis 8.8.0 /
Search 80808, container four CPUs/eight GiB; Apple M5 Max, 18 logical CPUs,
36 GiB, macOS 26.7.3 arm64. Diagnostic Go tests used GOMAXPROCS=4. No concurrent
heavy work ran during these timing windows. Raw operational identifiers/logs remain private.

[Focused race checks](checks.json) passed **23 tests, zero skips/failures**, including
actual Software large-value and temporal update/delete/type changes between
all-NULL HMGET and fallback, exact int64/NUL/empty strings, source-key precedence,
request bounds, cancellation and unknown/composed outer/fallback outcomes.
Independent source review found no actionable correctness finding. Human review
and merge remain separate. All Go processes/clients exited and the owned
harness cleared its data/indexes before returning the shared fixture slot.

**Sparse/wide tradeoff:** an all-selected-NULL row pays HMGET plus HGETALL and can
fetch large unselected fields. The dense non-NULL benchmark does not establish
sparse/wide-row performance; those cases may be slower than the retired guarded
script. No new sparse/wide speedup, production SLA, native matrix result,
ClickBench, Flex, native Cluster API or bulk/transfer measurement is claimed.
The parent combined native matrix remains separate evidence with its own source.

Run the complete offline audit:

```sh
python3 benchmarks/unmanaged-projection/audit.py
```

Reproduction uses the frozen test harness on a new, disposable Redis Software
fixture created by `make -C go redis-up`, independently verified for the stated
settings. The opt-in tests are `TestUnmanagedProjectionConcurrencyProfile` and
`TestUnmanagedProjectionSQLConcurrencyProfile`; run only in a coordinated slot,
with Go caching disabled and packages sequential. Never use a shared production
database: the owned snapshot harness clears its fixture.
