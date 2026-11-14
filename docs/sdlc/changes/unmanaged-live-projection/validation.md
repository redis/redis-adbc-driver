# Validation and review

Date: November 14, 2026 (America/Los_Angeles). Author/reviewer: Codex.
[Intent](intent.md), [spec](spec.md), [plan](plan.md). Human review pending.

- Focused unit tests pass: exact values/true NUL, existing all-NULL projection,
  native missing-script repair, typed TYPE/HMGET/RO-command permission fallback,
  no retries on mixed unknown/missing-script outcomes, malformed replies,
  unrelated server errors, untyped permission text, cancellation, driver-prefix
  refusal, field/request bounds and unaccounted pipeline errors with populated
  replies. Every script pipeline command retains NoRetry.
- Parent-owned isolated Redis Software fixture independently verified before
  each live command: active/effective 4x QPF, six Search workers, mnp/32,
  4-GiB noeviction shard, strict data TLS/RESP3. Client GOMAXPROCS=4. Operational
  identities and logs remain private.
- Six focused Software top-level tests pass in 4.691s: new native projected read,
  actual-NUL Arrow input, unmanaged aggregate inputs, foreign filtered membership,
  filtered live changed/deleted/wrongtype candidates, and filtered Arrow
  finish/release/cancel lifetime. Both EVAL_RO and EVALSHA_RO execute natively.
  Returned values above 2^53 and MinInt64 remain exact; application fields and
  source keys remain unchanged. Existing all-selected-fields-NULL HASHes survive.
- Test-only retrieval prototype (no Search/SQL/ADBC/FFI): matched one warmup plus
  three alternating measured rounds, requesting two native fields, full value
  and group/sum validation. Median 10K reads from five-field HASHes: HGETALL
  135.64ms versus TYPE+HMGET 39.47ms; 100K reads 1475.81ms versus 281.96ms.
  From 106-field HASHes, 10K reads 276.00ms versus 39.03ms. These are attribution
  observations under concurrent compilation, not production/native query claims.
- Instrumented full Go ADBC execution/Arrow profile uses 100K rows, four public
  native columns plus ordinal; source index only ordinal SORTABLE. Candidate
  filtered COUNT/SUM over 10K matching rows: measured rounds 94.55, 94.49 and
  106.76ms. Same-fixture merged-main baseline: 243.75, 242.54 and 243.83ms.
  Medians 243.75 -> 94.55ms (2.58x). Every returned Arrow INT64 type/value and
  complete grouped result matched independent arithmetic. This excludes the
  Python/C boundary and is not the final integrated native matrix.
- Profile transfer drops from about 2.02MB to 1.06MB per filtered query. Command
  count remains 10K per-key reads plus Search discovery; this is not fanout
  elimination. Allocation rises from about 18.8MB to 21.0MB per filtered call;
  that cost remains visible. Unchanged grouped LOAD-star timing stays around
  390ms; this change does not claim to improve that path.
- The baseline profile temporarily restored exact HEAD query.go and restored
  candidate bytes in a finally block; private SHA256 proof confirms restoration.
  All live clients/processes exited before releasing the exclusive fixture slot.
  Final source-key/actual-address and unaccounted-error guards are checked by
  focused unit follow-ups, rather than relabeling the earlier timed source.
- Unit-only `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...`
  passes across every package; integration skips in this command are not native
  compatibility evidence. Final focused unit follow-ups pass separately.
- gofmt and git diff --check pass. Parent integration records full native
  one/four-client before/after timing, final combined-source validation and PR.

Cross-review found that errors.Is(err, goredis.Nil) could hide a lost transport
reply joined with a nil marker. Six new cases reproduced acceptance or replay on
84745e2d before the fix. Absence now requires direct equality with goredis.Nil;
pipeline accounting compares only direct nil/native outcomes and never unwraps
joined/wrapped failures. Ten new scenarios cover command/pipeline joined or
wrapped nil, known NOSCRIPT/NOPERM preceding a later unknown, and composed native
refusals. Focused `go test -race -count=1 -p 1 -run
'^TestUnmanagedProjectionRead' .` passes with Redis environments unset. Original
unknown diagnostics remain present and all these cases issue only one pipeline,
with no repair or compatibility fallback. This is a correctness follow-up;
earlier timed sources and live evidence are not relabeled.

Review checked atomic existence/value semantics, exact raw values, bounded
one-key routing, permission fallback proof, source isolation, malformed/unknown
outcomes, cancellation and retained Arrow/cursor ownership. The cross-review
finding is fixed; no actionable finding remains in this scoped agent review. Agent review is not human approval. No new
100M ClickBench, Flex or Flight SQL benchmark ran.


## Selected-value resource follow-up

Source review found that bounded requests alone permit arbitrarily large selected
external HASH values to be copied into Lua. Redis's
[bulk-to-Lua conversion](https://github.com/redis/redis/blob/8.0/src/script_lua.c#L253-L266)
uses lua_pushlstring, whose [Lua contract](https://www.lua.org/manual/5.1/manual.html#lua_pushlstring)
creates or reuses internal string storage. Eight selected 64-MiB fields can thus
introduce about 512 MiB of VM payload even though the field-name request is tiny.
The managed writer's limit does not constrain external application HASHes.

The RO script now checks cumulative selected HSTRLEN values plus small per-slot
budget before HMGET, declining above 1 MiB. HSTRLEN is a native read-only
[constant-time length query supported on Redis Software](https://redis.io/docs/latest/commands/hstrlen/).
The check and read share one atomic script, so a writer cannot replace a small
value with a large one between them. The fixed direct native size refusal reads
only that row through original HGETALL instead; no data are rejected/truncated
and later small rows retain projection. Missing HSTRLEN permission selects the
existing permission compatibility path. Unknown/composed errors never prove a
safe size/permission fallback, and all pipeline outcomes are inspected first.

New authored tests cover per-row-only size fallback, HSTRLEN denial, mixed
success/NULL/large rows, later fast-path reuse, composed size refusals, a known
size/permission refusal followed by an unknown reply, and unaccounted pipeline
size errors. The native boundary test uses modest values above the threshold,
cumulative individually-small fields, an exact boundary, missing selected fields
and large unselected fields; it checks native guard refusal and complete original
HGETALL data, rather than attempting an OOM-sized fixture.

At resource-guard authoring, these tests were not executed because the parent's
ordinary suite owned the live slot and builds were paused. Parent integration
must run focused native coverage and final before/after native timings with this
extra HSTRLEN work. Earlier timing and validation observations
above retain their actual prior sources and are not final guarded-source claims.


## Native refusal spelling follow-up

Parent integration's native boundary test on combined source 00553f22 exposed
Redis's actual error spelling: redis.error_reply of the fixed single-token size
refusal arrives as `ERR ADBC_UNMANAGED_PROJECTION_TOO_LARGE`. The classifier now
accepts only the exact bare token used by deterministic models or the exact token
with one `ERR ` prefix. The same correction covers TYPE, HMGET and HSTRLEN
preflight tokens. Top-level NOPERM matching is unchanged; no arbitrary prefix
stripping or exception unwrapping was added.

Focused unit/race coverage includes all four tokens with both spellings, native
permission fallback and per-row size fallback using actual prefixed wire forms,
and rejection of suffixes/infixes/doubled prefixes/untyped strings and
wrapped/joined prefixed errors. Unknown prefixed outcomes preserve their original
diagnostic and cause exactly one pipeline with no fallback or repair. The full
`TestUnmanagedProjectionRead` focused race selection passes with both Redis
endpoint environments unset. No live command ran during this correction; parent
integration retains the red native checkpoint and must repeat verified native
boundary/lifecycle coverage before final timings.

## Final integrated follow-up

The [November 14 combined validation](../point-write-retrieval-20261114/validation.md)
records the final guarded native build, complete paired matrix, corrected boundary
failure, independent review and ownership cleanup. Earlier diagnostics above retain
their actual sources and scope; they are not relabeled as final native measurements.

Implementation and evidence: [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).
