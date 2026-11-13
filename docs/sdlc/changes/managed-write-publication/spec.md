# Contract

See [intent](intent.md) and [plan](plan.md). Profile complete 10K x 105 managed Arrow writes with automatic maintenance, and separately attribute deterministic compaction steps. Validate every output value, NULL, exact BIGINT and row multiplicity outside timing. Foreground command times do not measure exclusive server CPU or background work.

Select an optimization only from measured costs. Keep bounded work, original acknowledgements and unknown-outcome handling; never combine separate input calls or weaken publication/readiness proofs. Record all samples and comparison limitations.

## Selected change

Reuse the existing standalone control executor for the existing row-image,
index-notification and atomic-compaction-copy scripts. Keep one EVAL-only
compatibility state per store/script. On a definite native NOSCRIPT or top-level
EVALSHA denial, send one full-body EVAL with identical keys and arguments.
Do not retry transport, wrapped/joined, Lua-runtime or key-permission errors.
Check cancellation before fallback and preserve NoRetry on both attempts.

The executor owns each fresh argument slice, changes only its script header,
and copies only for a fallback so observers retain the original wire command.
No script body, storage format, pipeline behavior, row/byte bound, catalog pin,
publication proof or durable outcome changes. Cache loss is expected.

Acceptance separates measured script-header bytes from total traffic and
end-to-end throughput. Timing may remain unchanged; do not claim otherwise.

PR: [#410](https://github.com/redis/redis-adbc-driver/pull/410).

## Native cancellation coverage

C-ABI tests separately cancel an unordered managed directory scan and a Search scan
with an explicit fixture-sized LIMIT. Retain the cap-aware cursor expectations on the Search path, all
cancellation/error/cleanup assertions, and complete statement-reuse validation.
Always unblock and join the reader on an earlier test failure. This is a test
correction, without production or workflow changes.
