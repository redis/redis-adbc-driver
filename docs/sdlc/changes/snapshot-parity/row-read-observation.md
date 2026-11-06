# Statement-scoped row-read observation

Date: 2026-11-04. Parent: 0f53a69. Status: focused validation passed.
PR: https://github.com/redis/redis-adbc-driver/pull/284.
See [intent](intent.md), [spec](spec.md), [plan](plan.md).

The hosted macOS failures in [the full checkpoint](full-815-validation.md)
reported HMGET calls for queries whose predicates cannot match. Immutable metadata
resolution uses HMGET on `snapshotMetadataGraph` to prove bucket cardinalities,
object presence and reference counts. Global command counters conflate these
proofs with row HASH fetches, and also count unrelated background connections.
Software blocks CONFIG RESETSTAT, so those assertions previously skipped there.

The test helper now observes the complete callback execution/fetch through hooks
on both the maintenance and execution clients. Each observation creates and
closes an execution transport; only its tagged context contributes counts.
The fixed metadata graph key is the sole HMGET exemption. Other HMGET keys and
the existing FT.AGGREGATE, FT.SEARCH, FT.CURSOR, FT.PROFILE and SCAN categories
remain counted. No driver query, metadata validation or production command is
changed to make counters look smaller. The public query assertions and independent
SQL result expectations remain intact.

Cluster parent and current/new node hooks use a shared context marker to avoid
double counting routed calls while observing direct Search-node calls. Hook
installation on a node is deduplicated. Actual Cluster API validation remains
open; testing through a three-shard Software proxy does not establish it.

Observation no longer needs CONFIG/INFO statistics permissions. The old three
attempts to find a quiet global-counter interval are removed; each assertion now
checks one execution. The existing full-scan and exact row-ID positive controls
must still observe FT.AGGREGATE and HMGET respectively. Join-versus-single-table
command comparisons, empty SELECT/ExecuteSchema/write checks and false row-value
predicate checks all retain their expectations.

## Evidence

The first scoped-observer run, before distinguishing the fixed metadata key,
reproduced `TestSQLEmptyReadsNothing` reporting HMGET reads on an empty query.
The next run passed both `TestSQLEmptyReadsNothing` (1.670 seconds) and
`TestSQLRowValuePushdown` (0.640 seconds), including positive controls; root package
4.138 seconds. Final per-observation transport validation passed both tests with zero test skips/failures; root package 4.856 seconds. `git diff --check` passed.
All runs use `make -C go test-software` with `-race -json -failfast`, selecting
those test names, on verified active/effective Software 4x QPF/noeviction,
Redis 8.8.0 with three primary shards behind the Software proxy. No Community
fixture was launched. A first attempted command had an unescaped Make dollar
anchor and failed in the shell before tests; that is not a test result.

Private logs preserve command output. No hosted rerun, full-suite result,
Cluster API recovery proof or performance claim is inferred from this selection.
Authorization, process/input resume, sustained compaction, broader platform/
recovery coverage and current matched benchmarks remain separate open gates.

This observes client commands; it is not a count of shard-internal commands or
a general detector of arbitrary row reads inside future Lua scripts. Current
query row fetches use the observed commands.
