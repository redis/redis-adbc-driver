# Intent

Requested by Albert Wong on 2026-11-12: build the unmanaged Arrow ingestion gap
identified during the v0.0.26 comparison. Base: v0.0.26 / 5c9d583a.

Allow Arrow producers to append rows directly to an existing registered
application HASH table without SQL literal construction. Preserve application
ownership, existing-key refusal, encoding and no-replay guarantees. Managed
Arrow snapshot behavior remains intact. See [spec](spec.md) and [plan](plan.md).
