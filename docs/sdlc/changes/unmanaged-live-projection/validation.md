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

Review checked atomic existence/value semantics, exact raw values, bounded
one-key routing, permission fallback proof, source isolation, malformed/unknown
outcomes, cancellation and retained Arrow/cursor ownership. No actionable finding
remains in this scoped agent review. Agent review is not human approval. No new
100M ClickBench, Flex or Flight SQL benchmark ran.
