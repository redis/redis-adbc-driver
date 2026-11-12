# Flex snapshot spec

Use exact immutable row directories and HASH values for Flex snapshot reads, merging versions/tombstones before SQL evaluation. Build final plain indexes with SKIPINITIALSCAN and guarded notifications; require warning-free native completion proof before publication. Require FT.SEARCH for Flex writers. Preserve COW row counters and atomic numeric-ID bounds. Recover missing private compaction indexes by retiring the job and rebuilding fresh output from retained sources; never initial-scan Flex or replay unknown writes.

Related: [intent](intent.md), [spec](spec.md), [plan](plan.md).

Pull request: [#395](https://github.com/redis/redis-adbc-driver/pull/395).
