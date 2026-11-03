# Next: incremental row references and independent publication

Status: incremental row references implemented locally in storage format 9;
local race/native validation passed; see [evidence](row-reference-validation.md). Reservation no longer copies `r:<base>` and publication
no longer enumerates table descriptors. The data registry/counters share the
metadata graph, with a separate durable zero-reference queue. Independent
publication and bounded bucket construction remain pending. Do not claim full
constant-catalog-cost writes or throughput parity without measurements. See
[metadata direction](metadata-design.md).

The metadata graph should also own immutable table-descriptor-to-row-data edges.
A native metadata object's first live reference retains its segment IDs once;
additional bucket aliases do not duplicate those edges. Value reclamation drops
its data edges only when its final bucket reference disappears. Views and other
metadata do not contain direct row-data edges. Staging owns its new data IDs,
and a durable compaction job independently retains its input/output IDs.

Copying a table metadata value must attach its source data edges **before** the
bucket stops pointing at the source object. Do not leave the copied descriptor
without data ownership until final sealing: a base pin could expire before a
slightly later staging lease, allowing source data reclamation. Changes to a
private table descriptor must update its data edges in the same writer fence as
the native JSON SET. Validate every data generation is live before mutating any
edge; reject references to a deleting/reclaimed generation.

Move data ownership/counters out of the catalog hash so ordinary control scripts
scan only bounded pins/work records. Staging work references should list only
owned outputs; release them durably after committed metadata objects protect the
data, or during aborted/expired retirement. Compaction references remain bounded
by one source lineage and output. A durable zero-reference data queue admits row,
index and directory reclamation without rescanning every live catalog reference.
All keys must remain explicit and share the metadata slot. Preserve row/index
writer admission, uncertain publication outcome handling and no input replay.

After row-reference publication is incremental, implement metadata-only rebasing
for unrelated concurrent publications. Record versions of metadata actually read
by SQL/Arrow planning, separately from housekeeping. Compare those versions in
the latest pinned root; conflicts in a read/target table still fail. Merge dirty
logical keys into latest buckets, including collisions with other dirty keys in
the same bucket; replacing an entire old bucket would lose unrelated updates.
Never re-evaluate SQL, defaults, volatile expressions, parameters or Arrow input.

Keep original and merge-base pins until the outcome is known. The original owner
and latest CAS base are different identities: publication must validate both
without confusing the writer token with the merge pin. Seal private row/metadata
outputs before rebasing; after sealing, stale work must not mutate them. Consider
bounded/restart-safe bucket construction as catalogs grow beyond the fixed-map
microbenchmarks.

Acceptance: concurrent different-table writes commit; same-table and read-source
changes conflict; old streams/live partitions survive GC; source rows remain
protected during private metadata replacement and expired lease takeover; aborted
or unknown publication never exposes partial input; failover/AOF recovery retains
both edges and outcomes. Benchmark many-table writes and metadata/row retention,
not just physical copy command counts. Full feature/native/Cluster matrix and
remaining large-operation/compaction work stay required.
