# Snapshot-consistent ADBC statistics

Date: 2026-11-04. Parent 0e60235. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The fresh 150-name ordinary continuation stops immediately in
TestStatisticsManagedCountsAndFilters (root 0.837 s): GetStatistics creates an
execution transport but never pins catalog discovery/counting. Its public API
therefore fails on the first unscoped table-list read. The former count paths
also inspect only the current physical index/prefix, without resolving older
segments, replacements or tombstones.

GetStatistics now pins once before catalog discovery and retains that generation
through all schema/table/count reads and final lease validation. Nested metadata
calls reuse the same scope. Arrow statistics are materialized before the pin
releases; success and error paths release it before returning. Statistics planning
does not reset a connection's unrelated query metrics.

Approximate requests use the existing immutable visibility-mask proof and Search
count queries, including for a single-source baseline. Replaced versions and
tombstones cannot contribute. Invalid counts or overflow fail explicitly. IDs
outside exact Search precision or excess mask work fall back to authoritative
ordered directories. Exact requests use that directory merge directly, fetching
only winning row identities/tombstones in bounded pages. The one-minute per-table
deadline and two-million physical-entry work limit remain; the latter includes
shadowed/deleted entries rather than counting only visible output. Neither path
returns a partial count on error. The canonical Arrow schema, filters, eligible
layouts and conservative approximate flag retain their API contract.

Regression phases cover empty/INSERT/UPDATE/DELETE/append/TRUNCATE counts in both
request modes. Mid-count TRUNCATE/DROP/recreation of two tables plus GC must leave
one consistent old pair, then a fresh call must see the new pair. Tests assert
pin release before consuming the materialized Arrow output. Source denials on
both paths preserve I/O/NOPERM and return no reader or leaked pin. Sparse row IDs
above 2^53 must count correctly even with Search counting denied.

## Validation and review

Owned strict-TLS Redis Software, noeviction and effective active 4x QPF verified
before every Redis selection; mutating selections run sequentially.

- Final statistics race selection: eight pass events, one explicit ACL setup
  skip, zero failures; root 3.956 s. Software rejects protocol ACL SETUSER, so
  the existing read-only-principal case remains unverified. Injected source
  denials are not proof of a complete restricted-principal command recipe.
- Unit-only packages: 952 pass events, 859 expected integration skips, zero
  failures; root 26.295 s. Those skips do not establish Redis compatibility.
- Rebuilt native library using make build's version linker flag: the existing
  C/ADBC statistics pytest case passes, zero skips/failures; 0.70 s. Python
  3.14.10 and upstream validation source 5db1f44782f4868c5ae9e89d6ac9812ca6e1758c.
  This checks complete canonical nested Arrow results, row counts and filters.

The initial native attempt could not load conftest because the standalone
Python 3.13 snapshot environment lacked adbc_drivers_validation. The repository's
declared Python 3.14 validation environment was prepared and used for the passing
case; no requirements or tracked lockfile were changed. Initial observation hooks
missed baseline counts because the first implementation correctly fell back to
directories but did not optimize non-overlay baselines; the final implementation
uses the same mask proof for those baselines. The CUDA package has no applicable
selected tests and is skipped. Formatting and diff whitespace checks pass.

Reviewed nested pin reuse, read-only discovery policy, complete result
materialization, cross-table DDL consistency, visibility before count, baseline
Search optimization, exact sparse IDs, bounded directory work, overflow,
error/permission propagation, retained API flags and pin/Arrow ownership. No
protocol, ACL grants, source mutation or migration changes. Author review is not
independent maintainer approval. Full ordinary/platform, current-protocol Cluster
recovery, restricted-reader, public input/process resumption, sustained compaction
and comparative performance gates remain open.
