# Store Arrow Map and Union values

Date: 2026-11-04. Requestor: repository user in the active Codex chat.
Base: `f1e1f03` on public `redis/redis-adbc-driver` main.
Status: implemented and locally validated; PR/CI verification follows.

Complete [#267](https://github.com/redis/redis-adbc-driver/issues/267) and
[#268](https://github.com/redis/redis-adbc-driver/issues/268) by replacing the
previous deterministic rejection with explicit, persisted SQL container types.
Arrow ingestion, fresh connections, ordinary queries, native C and IPC must
agree on values and canonical child schemas. Preserve duplicate Map keys,
ordering and selected-child Union NULL tags. Reject invalid buffers and
incompatible schemas before allocating decoded rows or writing a batch.

Retained partitions explicitly reject these new types, including nested uses;
live partitions support independent replay. No Search indexing of containers,
implicit text/JSON conversion, schema evolution, new transaction guarantees or
new topology claims are included. See [spec](spec.md) and [plan/evidence](plan.md).
