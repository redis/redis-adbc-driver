# Timestamp storage inspection under one snapshot

Date: 2026-11-03. Parent 684ef29. Production remains protocol 15.
Full [feature and performance parity](plan.md) remains incomplete.

## Reproduction and correction

The ordinary diagnostic at the previous checkpoint stops at
TestFractionalPrecisionIssue113 after 462 pass events and ten capability skips.
Its rowKeyOf helper queries an ID, reads metadata through an unscoped legacy
store, then assumes the newest segment contains that row. It also converts a
logical integer ID to a single rune instead of decimal SQL text.

Stored-row inspection now resolves the logical SQL ID and fetches the newest
visible row image under one pin. No physical key escapes after pin release; the
caller receives owned decoded HASH strings. Exact stored timestamp/time unit
assertions retain their original expected bytes. The visibility regression uses
multi-digit and negative logical IDs, an exact replacement above 2^53, an older
segment row and NULL, with complete ordinary SQL results checked independently.

The first focused run has six passes and one failure (root package 11.747
seconds): TestSessionTimeZoneIndexAndIsolation has a separate unscoped planner
helper. It now plans and checks pushdown under one pinned catalog while retaining
the requested session time zone. Exact query ranges/residuals, literal casts,
DST transitions, Arrow units and values, ingestion and session isolation retain
their existing assertions. Production code and storage are unchanged.

## Verification

Owned strict-TLS Redis Software, Redis 8.6.2, three primaries, Search, noeviction
and active effective 4x QPF verified through make -C go test-software. Mutating
suites run sequentially on macOS ARM64. No ClickBench run/reload. Operational
artifacts, credentials and infrastructure identifiers remain private.

- Final timestamp precision, rounding, session-zone and stored-row selection
  with `-race -count=1 -p 1`: seven top-level passes, zero skips/failures;
  root package 17.028 seconds.
- Unit-only packages: 923 pass events, 823 expected integration skips, zero
  failures. The additional skip is the new server-dependent inspection case;
  skipped integration tests do not establish compatibility.
- Subsequent ordinary diagnostic with `-json -failfast -count=1 -p 1 -skip
  TestACLAppIndexReader`: 491 pass events, ten capability skips and two failure
  events (one child and its parent), root package 179.049 seconds. It stops at
  TestSQLRenameDuringDrop/before_the_index_is_dropped/plain, whose DROP receives
  40001 after intervening metadata publication. That race fixture and later
  cases remain open. The ACL exclusion is command-line-only; no repository skip
  is added and complete ordinary success is not claimed.
- Direct `gh pr checks` inspection reports no checks on the current branch at
  inspection. Empty commit-status results alone were not treated as proof that
  hosted validation ran or succeeded.
- Go formatting, local evidence links and diff whitespace checks pass.

## Review and remaining work

Author review covers one-pin metadata/row resolution, canonical physical IDs,
exact native bytes, NULL absence, older-segment visibility, planner session state,
full result consumption and lease/cursor cleanup. This is not independent
maintainer approval. Full ordinary/current Cluster/platform/recovery, restricted
reader ACL integration, process resume, sustained compaction and broader
performance comparisons remain required. Protocol 16 is not activated.
