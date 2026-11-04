# Bound numeric schema reconciliation

Date: 2026-11-04. Parent 4316633. Production remains protocol 15.
Full [feature and performance parity](plan.md) remains incomplete.

## Reproduction and correction

The ordinary diagnostic after narrow scans stops at
TestSQLBoundDecimalDivisionScales: four pass events, one profiling skip, one
failure, root package 1.283 seconds. The result-schema guard compares already
resolved Decimal precision/scale between parameter rows and rejects valid
value-dependent scales of the same declared NUMERIC expression. It also leaves
the combined result tied to the first row's resolved type.

Finite query results now retain their immutable declared column slice separately
from the output slice copied for numeric resolution. Bound execution compares
declared shapes under each execution's own pin and combines values using that
declared shape. Final fitting resolves one common exact precision/scale from all
rows before Arrow construction. No extra column copy is introduced beyond the
existing fitResult copy. The cached plan's types remain unchanged. Fixed-type,
name and column-count changes retain the existing error; no SQL or input replay
is added and the per-execution snapshot boundary is unchanged.

The unchanged division test requires exact 10000 and 0.00000001 values in one
Decimal128(33,28) batch and now passes. A new real-ALTER regression acquires the
first read pin, publishes INTEGER-to-TEXT ALTER through another public connection
and then runs both bound rows. The first execution retains its old declared type;
the second sees the new type, and the combined query must still reject the
declared schema change. The observer delegates actual Redis control calls on an
idle no-worker maintenance client; it never fabricates metadata or schema replies.

## Verification

Owned strict-TLS Redis Software, Redis 8.6.2, three primary shards, Search,
noeviction and effective active 4x QPF verified before database selections.
Mutating suites run sequentially on macOS ARM64. Raw logs, endpoint identifiers
and operational artifacts remain private. No ClickBench run or reload.

The original division and division-schema tests pass two race events with no
skips/failures. A broader numeric/bound selection passes six events, including
the genuine schema-change guard, and fails TestSQLNumericNotEqualIndex's old
missing-default residual assertion, root package 12.617 seconds. That separate
fixture assumes ALTER leaves virtual missing defaults; atomic ALTER physically
materializes them. Its SQL rows/counts remain correct. Its follow-up preserves
the fallback assertion on a private descriptor clone and separately checks the
published column has no missing-value marker.

Unit packages pass 946 events with 845 expected integration skips, zero failures,
root package 26.405 seconds. Skips do not prove database compatibility. The native
library is rebuilt with make -C go build. All 16 native C/ADBC snapshot tests pass,
zero skips/failures, in 14.181 seconds. The new native bound-numeric test fully
fetches exact decimal values and NULL with both parameter orders and checks the
combined Decimal128(33,28) schema.

The complete snapshot race selection (`^TestSnapshot`, `-race -count=1 -p 1`)
passes 254 events, 21 explicit skips and zero failures; root package 304.072
seconds. The skips are nine Cluster API cases, six permission cases and six
separately gated reader-model cases, not compatibility passes. The corrected
eight-case numeric/bound/timestamp race selection passes eight events, zero
skips/failures, root package 13.567 seconds. It verifies both the materialized
default range-union optimization and the private virtual-default residual guard.
Full ordinary/platform/Cluster/recovery, explicit process resume, sustained
compaction and comparative performance gates remain open.

Author review inspected declared versus resolved type lifetime, plan immutability,
combined fitting, exact values/NULLs, real intervening DDL, reader cleanup and
unchanged execution/publication boundaries. This is not maintainer approval.
