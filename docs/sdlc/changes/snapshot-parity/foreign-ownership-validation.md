# Foreign indexes and private CREATE reclamation

Date: 2026-11-03. Parent 42cd4c2. Production remains protocol 15.
Full [feature and performance parity](plan.md) remains incomplete.

## Reproduction and required ownership behavior

The focused original TestSQLForeignIndexName fails before SQL CREATE at its
unscoped lastNames lookup: snapshot catalog access requires a statement
snapshot. The earlier ordinary diagnostic stopped at that same fixture.
Legacy mutable name counters cannot predict immutable UUID generation names.

The restored fixture creates application-owned indexes on separate and legacy
table-like prefixes, including an empty filtered index. CREATE, TRUNCATE and
forced-rekey RENAME allocate distinct canonical generation names. The external
indexes and exact HASH contents survive those statements, DROP and explicit GC;
the filtered index retains its original prefix and filter.

The old empty-leftover test expected an unowned index to be deleted and reused.
An empty index alone provides no ownership proof. Its replacement checks that
an unowned empty index survives, while a real registered private CREATE output
can be reclaimed. The test injects one error after the server actually creates
the final private index, verifies the published root and table absence, aborts
the workspace and checks reference-aware reclamation of the index, row directory
and ownership fields. A fresh explicit CREATE uses neither the failed output's
identity nor the application's storage. INSERT/read/DROP then completes and the
application index still survives. SQL and uncertain index operations are not
replayed.

The ACL fixture also removes its legacy counter dependency and requires a fresh
canonical private identity. Existing command permissions are unchanged; this
does not implement the pending restricted-reader protocol redesign.

## Verification

Owned strict-TLS Redis Software with Redis 8.6.2, three primaries, Search,
noeviction and effective active 4x QPF verified by make -C go test-software.
Mutating suites run sequentially on macOS ARM64. No ClickBench run/reload.
Operational identifiers, credentials and raw logs remain private.

Focused ownership selection with `-race -count=1 -p 1` exits zero: two passes,
one explicit ACL administration skip, zero failures; root package 3.117 seconds.
Software disallows protocol ACL SETUSER; the skipped case is not restricted-role
evidence. The first iteration's assertion used the staged placeholder index
instead of the actual final sealing index; the final test verifies the actual
created index's canonical identity and exact private prefix.

After adding the successful INSERT segment's explicit reclamation assertion,
the final focused selection again exits zero: two passes, the same one ACL
administration skip, zero failures; root package 3.182 seconds.

Unit packages exit zero: 923 pass events, 823 expected integration skips and no
failures. Unit integration skips do not establish compatibility. Production
behavior and storage format are unchanged.

The subsequent ordinary diagnostic (`-json -failfast -count=1 -p 1 -skip
TestACLAppIndexReader`) passes the restored foreign ownership cases and reaches
623 pass events, 14 explicit skips and one failure; root package 307.269 seconds.
It stops at TestSQLFunctionsStoredBefore's unscoped putView fixture, before that
test's invalid-function assertions. Later cases did not run. Skips include
profiling gates and Software ACL administration limits. The sole command-line
ACL exclusion is unchanged; full ordinary success is not claimed.

GitHub also reports the draft PR conflicting with current main. A non-mutating
merge-tree preview finds conflicts in connection shutdown, scan execution and
transport cleanup. The incoming prefetch/device changes have not yet been
integrated or validated with this snapshot branch. Earlier results do not prove
compatibility with that incoming code.

## Author review and remaining work

Reviewed ownership proofs, external HASH/filter preservation, failed private
index cleanup, fresh identity allocation, exact SQL results and explicit ACL
limitations. This is not independent maintainer approval. Full ordinary,
restricted reader ACL, current Cluster/platform/recovery, process resume,
sustained compaction and comparative performance remain open.
