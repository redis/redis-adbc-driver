# Reachable utility audit

Date: 2026-11-06. Base: `9c838db7`. Issue: [#341](https://github.com/redis/redis-adbc-driver/issues/341).

The historical statement that supported helpers still stage row images through
the cumulative 100,000-HSET guard is stale. The guard remains defensive internal
code in `scopeSnapshotCommand`; the audited public entry points below do not use
that row-image path. Their metadata/control commands still have a cumulative
64 MiB admission budget. Native row batches, import journal pages and compaction
copies enforce their own per-packet limits; this is not unlimited operation size.

| Entry point | Current row/control execution | Client memory/input retention | Failure/publication boundary |
| --- | --- | --- | --- |
| `CopyHashes` / CLI copy | `hashScan` → Arrow ingestion → `writeSnapshotRows` / native row batches; 256 images / 1 MiB target, 64 MiB single-packet ceiling | Up to 10,000 rows / 200,000 values per Arrow record; ingest conversion slices at 4,096 rows. The reader retains its record while consumed. Key deduplication is an operation-sized map. HASH replies/source pages are row-bounded, not byte-bounded. Sampling `-1` retains the whole schema sample. VARCHAR requires valid Arrow UTF-8; BYTEA carries arbitrary bytes. | Every mode uses one final publication after complete input/index validation. Cancellation/conversion/unknown failures abort private output; an existing append/replace target remains. Source HASHes are read only. |
| `AdoptHashes` / CLI adoption and repeated Apply refresh | Always dispatches to `snapshotAdopt`, then `importSnapshotTable`; reference-owned source journal pages precede bounded native output. No in-place application adoption | Planning still retains a seen-key map and schema facts. Capture Apply deduplicates in the private journal, using 128-row / 1 MiB target groups and a 64 MiB hard packet ceiling. Source discovery/fetch allocation is a separate row-count bound; no whole-process heap bound is promised. | One publication per copy/refresh. Journal capture mutations are never replayed after an uncertain reply; output replies resolve their durable outcome. Application HASHes/indexes are unchanged. |
| `RefreshForeignIndexes`, foreign-index import/refresh option | Same private journal and native row-image pipeline; membership/identity checks and exact native conversion precede final index readiness | Source fetches use 128-row page budgets, payload lengths and retained byte admission. Source-key deduplication is journal-backed. Topology/schema/membership changes fail explicitly. | Each index publishes separately; refreshing all indexes is not one multi-index transaction. Unknown capture aborts; uncertain output resolves its outcome. No external index/HASH mutation. |
| `InspectHashes` / CLI check | Reads source HASHes; existing managed catalogs acquire metadata pins/control maintenance | Sample keys/rows and report/schema facts remain retained; `Sample=-1` can materialize the full sample | No application mutation or managed row publication. Pin/maintenance control writes are not row-image staging. |
| `SweepReleased` / CLI sweep | Native reference-aware retirement and 256-row reclamation batches, at most 256 maintenance rounds per Apply | Bounded directory/manifest pages plus the current report | Preview suppresses connection-side reclamation. Apply reports confirmed deletion and partial errors; pins/active writers protect sources. Unknown deletion is not resent as an assumed success. |
| SQL ALTER and explicit rekey | `alterSnapshotTable` / `rekeySnapshotTable`, 256-image / 1 MiB target batches, guarded descriptor publication | Immutable scan/conversion pages; constraints and query planning can retain more input | One final publication; old pins retain original rows. Legacy `rewriteKeys`, DUMP/RESTORE rekey and Flex row copies are not this route. |
| CTAS / materialized view creation and refresh | Normal managed insert path, then guarded metadata publication | Query/result planning can materialize a complete result. Native write batching does not make that input streaming | One publication per statement. Failed refresh preserves the previous view. |
| CREATE/DROP/RENAME, temporary-object cleanup and other catalog-only utilities | Guarded private metadata commands and native reference updates | Catalog/descriptor and control-work admission remain bounded | No bulk row-image copying through HSET; publication and subsequent registered reclamation remain separate. |

The native INSERT/UPDATE/DELETE, Arrow, import and maintenance routes have already
replaced the old unbatched row loop. Legacy `writeKeyRows`, mutable adoption,
`writeRows`' non-snapshot branch, `writeUpdates`' non-overlay branch and Flex copies
are not reachable as managed utility writes. Flex is rejected during connection
configuration. This audit does not remove those historical implementation files
or claim that unsupported legacy formats can open or migrate.

## Corrected repeated control work

`raiseNeeds` compares each streamed page against its original descriptor. A
17-page import reproduced 19 descriptor SETs even though only two capability
transitions occurred. For a privately owned physical output, the correction
reads the current private descriptor, checks its identity and omits facts already
recorded successfully. It neither changes the input descriptor nor stores a
client acknowledgement cache. TAG ranks and NaN flags only increase within this
output, so later pages cannot require a weaker fact.

The same regression now requires three descriptor writes total, including final
output bookkeeping. Shared/published layouts retain the existing update protocol.
Lost metadata replies still abort without publication or SQL/input replay.

## Corrected result retention

The original `CopyHashes` success path returned `&s.stats`, an interior pointer
into its `hashScan`. Keeping that result also kept the reader's operation-sized
seen-key map and closed connection objects reachable. A weak-reference GC
regression reproduces this while deliberately keeping the result alive.

The success path now returns an independent statistics object. Its table/row
counts and field/null maps remain intact, and the same regression proves that
the input scan can be collected while its result remains live. This removes
accidental post-operation retention; it does not reduce the current Arrow
record or seen-key map needed while the copy is executing.

## Intentional remaining bounds

- Keep the 64 MiB cumulative metadata/control-command budget, including row-ID
  allocation commands. A sufficiently large operation can still exhaust it;
  the native row-image byte count is a different budget.
- Keep the native 64 MiB single-row packet ceiling, 256-row / 1 MiB write targets,
  128-row capture groups, manifest/pin/segment admission and statement deadlines.
- Copy/adoption planning key maps, schema samples, Arrow records and SQL planning
  can retain operation-sized input. The new evidence measures those costs; it
  does not establish constant heap/RSS or a throughput guarantee.
- Durable row outcomes and journal checkpoints do not provide a public resume
  token, persisted SQL plan or process-crash resume of unread input.

See [contract](spec.md) and [source-specific results](validation.md).
