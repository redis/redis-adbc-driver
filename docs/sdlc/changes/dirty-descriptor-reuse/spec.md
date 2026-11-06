# Descriptor reuse contract

See [intent](intent.md) and [plan](plan.md). Parent production: `617e282a`.

1. Keep the scoped native GET and metadata ownership/permission checks, JSON
   decoding and exact logical schema/name identity check.
2. Share the existing local metadata policies with ordinary `getTableMetadata`:
   deferred-index directory visibility, compatibility refusals, catalog OID
   hydration and the read-time fence. Ordinary metadata loading stays unchanged.
3. Call the complete `validateSnapshotTable` proof on the reused descriptor.
   Do not bypass access identity, provenance/segment shape or shared-source
   row/index/reference checks. Retain existing snapshot validation-cache rules.
4. Each call reads fresh persisted descriptor bytes. Do not cache across the
   index-sealing and final-validation stages, which can see different index
   names and descriptor state. Timestamp the actual authorized read, not later
   hydration; an earlier timestamp is conservative for legacy rekey fences.
5. Retain missing-descriptor handling and all corruption/read refusals. Never
   replay SQL or Arrow, mutate external sources or change publication outcomes.

Verify complete UPDATE/Arrow results and unrelated catalogs with controlled
command attribution and both-order native comparisons on effective-QPF4 Redis
Software. All builds/tests/profiles must stop before matched timing. Preserve
negative results and explicit skips; fewer calls alone do not prove a latency or
sustained-throughput gain. Broader performance parity and #345 stay separate.
