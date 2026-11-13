# Contract

Preserve exact values/NULLs, layered newest-row/tombstone visibility, source and
index ACLs, reader/writer leases, cancellation, bounded row batches, durable
outcomes and no uncertain-input replay. Keep all index readiness/publication
proofs and managed admission. No storage-format or public atomicity changes.
Profile client versus server/round-trip costs and larger Arrow calls separately
from unchanged-workload driver improvements. Any candidate paging change must
retain exact candidate identity, cap checks, bounded memory and complete tails.
Preserve historical evidence; no 100M ClickBench or physical Flex run.

See [intent](intent.md), [plan](plan.md).
