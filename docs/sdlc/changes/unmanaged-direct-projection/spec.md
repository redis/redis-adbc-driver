# Contract

Eligible explicit unmanaged registrations may bind bare columns, aliases and
`__key` directly into Arrow buffers. Source keys remain strings, including binary
bytes; arbitrary application `__rowid` fields are never interpreted as IDs.
Retain external storage encodings, NULL versus empty values, metadata-order
errors, output physical types, exact integer bytes, aliases and duplicates.

Keep existing LOAD*/HGETALL retrieval, Search membership, vanished/non-HASH row
handling, source/index checks and cursor cleanup. Application sources/indexes
must remain unchanged. Full predicates/residuals, expressions, late projection,
ties, unsupported layouts and Flex retain their existing complete paths.
Completed Arrow buffers must outlive Next, full/early Release and cancellation.
No public option, data layout, fetch/page bound or publication change.

See [intent](intent.md) and [plan](plan.md).
