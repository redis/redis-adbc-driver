# Retained partition storage foundation

This storage component supports opt-in retained ExecutePartitions for [#169](https://github.com/redis/redis-adbc-driver/issues/169),
[#166](https://github.com/redis/redis-adbc-driver/issues/166) and [#167](https://github.com/redis/redis-adbc-driver/issues/167). See [the public runtime contract](retained-partition-results.md)
for options, SQL/layout coverage, quotas and lifecycle. Live row-range partitions
remain the default.

A configured namespace is 1–64 ASCII letters/digits/underscores/hyphens.
Keys are driver-owned under `adbc:{retained:<namespace>}:`; braces cannot come
from caller input. Each job has a fresh random 128-bit identity and a separate
upload nonce. References carry namespace, job ID, manifest SHA-256 and absolute
expiry, without endpoints, credentials, source SQL or arbitrary Redis keys.
The reference grants no authorization: Redis authentication and namespace/job
key ACLs apply to every operation. A differently configured namespace is rejected.

All tenant keys share one Cluster slot, allowing atomic quota, publication and
revocation. This places a tenant's retained data on one primary; it is an explicit
scaling tradeoff, not a distributed storage throughput claim. OSS Cluster clock reads are
pinned to that primary; single-endpoint clients use endpoint TIME. Redis key
expiry remains authoritative. The default logical limits are 64 MiB output per job,
256 MiB per namespace, 1 MiB per chunk, 1,024 chunks and 1,024 active/tombstoned
jobs. Validated hard maxima include 1 GiB/job, 8 GiB/namespace, 16 MiB/chunk and
1,024 chunks/jobs. These bound driver-managed payload/state, not process RSS,
Redis allocator/index overhead, replication storage or malicious raw writes by
an authorized tenant publisher. Namespace limits cannot silently change across
publishers: the first configured policy is persisted and mismatches fail closed.

Control metadata and manifests are plain Redis Strings; quota ledgers are
Sorted Sets. No retained control state is a HASH or RedisJSON document. This
matters for application indexes whose prefix is empty: job creation must not
introduce new rows into an application's all-prefix HASH/JSON index.

A job reserves its output allowance before upload. Every provisional metadata
record and chunk receives the same absolute ten-minute lease from its first
write, with no renewal by upload or read. An abandoned producer cannot leave
immortal output. Expired reservations are pruned on the next quota operation;
small namespace control keys remain until operator cleanup. Registry cardinality
is bounded even when jobs are revoked repeatedly.

Commit checks the complete uploaded chunk set and publishes a checksummed
manifest last within one Lua operation. Each chunk has SHA-256, exact byte count
and declared row count; the manifest has schema fingerprint, totals and ordered
chunk metadata. All job keys receive a fixed shared server-clock expiry prepared
immediately before publication (one second to 24 hours). Publication returns
unused reserved bytes. Closing the producer does not delete a committed job.
No mutating script is transport-retried; uncertain/abandoned uploads retain the
bounded lease and cleanup path.

A reader rechecks state and manifest integrity before every chunk, then verifies
chunk size/checksum. GETRANGE bounds replies before allocation even if a chunk,
manifest or metadata record is oversized/corrupt. Errors distinguish expired,
revoked, missing/evicted and corrupt state; SQL is never rerun. Already downloaded
bytes are local values; expiry/revocation is checked again before the next chunk.

Revoke atomically marks the job revoked, removes its manifest and complete
bounded chunk key space, and returns payload quota. It does not use a racy
observed chunk count or a caller's smaller local limit. Uploads racing revoke
fail before creating later chunks. A small zero-payload tombstone counts against
the job limit until the original expiry, preventing unbounded metadata growth.
Abort is limited to its uploader nonce and does not revoke a committed uploader
object implicitly. Only job-owned keys are in the cleanup set.

The raw storage reader needs `TIME` and `GETRANGE` plus read access only to its
configured `adbc:{retained:<namespace>}:*` keys. Normal connection setup also
needs PING; Cluster clients need topology discovery/COMMAND and ASKING. It needs
no EVAL/write permission or source table/index reads. Publishers need the state
scripts and their declared namespace-scoped operations; they are trusted
publishers for that namespace. Public ADBC read-only bootstrap is exercised with independent minimal-ACL
worker processes. No TLS or RESP defaults change; final local storage
checks use RESP3 explicitly on standalone and three-primary Cluster clients.

The storage layer treats chunk bytes as opaque. Its Arrow IPC regression retains
exact int64 values above 2^53 through MaxInt64, binary bytes and NULLs. The private Arrow
reader now preflights bounded metadata, framing, nodes and buffer spans before
zero-copy reconstruction, rejects compressed/dictionary/extension payloads,
validates schema/row counts and fully validates reconstructed arrays. The public coordinator enforces transport/source/working/work/output admission
and provides global SQL, additional layouts and explicit revocation. Neither
the storage layer nor the coordinator provides atomic source snapshots,
exactly-once sinks or performance guarantees.

See [change records and evidence](sdlc/changes/retained-partition-store/evidence.md).


The [private reader component](sdlc/changes/retained-partition-reader/evidence.md)
uses one uncompressed Schema/RecordBatch/EOS stream per chunk, preserving exact
Arrow types and nested lists/records. It caps each chunk at 16 MiB, metadata at
1 MiB/message, 4,096 flattened fields, nesting depth 32, 1,048,576 rows per node,
and 16,777,216 summed node elements. Counts and offsets are checked before the
Arrow decoder; full validation is intentionally O(n). This is a bounded logical
format contract, not a process RSS guarantee. It follows the
[Arrow IPC format](https://arrow.apache.org/docs/format/Columnar.html#serialization-and-interprocess-communication-ipc)
with a restricted producer format; general compressed or dictionary IPC is not
accepted as retained chunks.

Readers load the persisted namespace policy with bounded GETRANGE, decode one
chunk at a time, and slice it to the configured batch size. They retain their
current Arrow batch until Next/Release; callers may retain it longer explicitly.
Cancellation/owner close releases local buffers and does not revoke the job.
The private version-2 descriptor carries only the retained reference and a
validated ordinal chunk range. It is wired into public ReadPartition with namespace validation and a
source-independent reader-only bootstrap.
