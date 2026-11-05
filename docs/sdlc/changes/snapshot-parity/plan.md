# Plan: snapshot feature and performance parity

Date: 2026-11-02. Status: in progress. See [intent](intent.md), [spec](spec.md).

The [released-owner checkpoint](released-owner-validation.md) records a
reproduced lost-reservation capacity leak and its ownership-aware reclamation
fix, plus the native snapshot rekey-close fixture correction. Local combined
checks pass; hosted verification remains required. This does not close reader authorization
or sustained throughput parity.

| Workstream | Status |
|---|---|
| Shared-generation live partition capabilities, release and expiry | Implemented; proxy/native checks passed |
| Visibility-correct Search predicates and parallel reads | Implemented; targeted and race checks passed |
| Exact index aggregation/grouping/top-K on segmented visibility | Implemented with bounded masks and exact fallbacks; seven-case read comparison recorded |
| Incremental metadata, independent publication and index lifecycle | Lazy validation and one-build index lifecycle/no-op publication implemented; pre-execution admission and metadata sharing/GC implemented; incremental row references verified locally; independent publication verified locally; bounded bucket construction verified locally |
| Large atomic writes/ingestion and sustained compaction | Durable dirty-table scheduling and GC batch progress verified locally; bounded SQL/Arrow row images and ALTER/rekey with durable outcomes implemented; process/input resume, remaining utility quotas and broader sustained throughput work remain open |
| Full feature/native/platform/Cluster API validation | Protocol-14 Cluster Go, primary promotion, all-node AOF/RDB restart and replicated proxy race/native checks verified historically; protocol-15 Cluster reruns, hosted CI and full feature/native/platform matrix remain open |
| Comparative benchmarks, review, PR evidence and owned cleanup | Seven-case read comparison and self-review recorded; remaining benchmarks/cleanup pending |

Implement and verify coherent milestones; keep PR #284 reviewable. Provision
only owned Redis Software fixtures with verified 4x QPF. Keep licenses,
credentials and operational identifiers private. Do not infer success from unit
integration skips or incomplete fetches. Record all remaining work explicitly.

The [foreign ownership checkpoint](foreign-ownership-validation.md) verifies
external index/HASH preservation and reclamation of a real failed private CREATE
output without identity reuse. Restricted-role and full-suite evidence remain open.

The [current-main prefetch checkpoint](current-main-prefetch-validation.md)
integrates incoming scan/device work while retaining snapshot visibility and
execution cleanup. Experimental directory/candidate prefetch and public wide
SQL/partition checks pass; full combined validation remains open.

The [stored-function and aggregate inspection checkpoint](stored-functions-validation.md)
restores private invalid-definition fixtures, checks pinned metadata/rows and
holds aggregate inspection on one generation. Full ordinary/platform parity remains open.

The [copy-check checkpoint](copy-check-validation.md) removes false blockers from
legacy physical-name prediction, verifies catalog-absent first copies and proves
external HASH/index ownership through private generation reclamation.

The [ALTER readiness checkpoint](alter-readiness-validation.md) restores private
sealing/publication fixtures and fixes recognized schema-convergence waits plus
terminal permission classification. Complete feature/platform parity remains open.

The [cleanup fallback checkpoint](cleanup-fallback-validation.md) fixes incomplete
tail-only reads and global sorting/limit guards, verifies sparse exact IDs and
preserves native retained capture plus shared-generation partition membership.

The [same-target ingest checkpoint](ingest-admission-validation.md) coordinates
append/create-append before input consumption while preserving independent
targets, cancellation, uncertain-reply errors and existing publication fences.

The [late-projection checkpoint](late-projection-validation.md) restores pinned
planner/cursor inspection, isolates the missing-HASH primitive and verifies
deferred exact projection reads across managed DML, DROP and GC.

The [NaN checkpoint](nan-validation.md) restores pinned flag inspection,
all-segment physical coverage, authoritative visible counts and reference-safe
reclamation, and tests raw application NaNs through a private managed copy.

The [narrow scan checkpoint](narrow-validation.md) retains exact pinned values
through DELETE/GC and preserves unsafe numeric fetch, cursor payload and
missing-HASH/cancellation checks on an owned mutable-source primitive.

The [bound numeric checkpoint](bound-numeric-validation.md) preserves declared
schema checks across parameter snapshots while reconciling value-dependent
decimal scales into one exact Arrow output schema.

The [parallel aggregate checkpoint](parallel-aggregate-validation.md) retains
full serial/parallel/automatic comparisons, shared worker budgets, tuning and
cancellation using atomic fixture writes and private scan-error plans.

The [balanced/default-prefetch checkpoint](balanced-prefetch-validation.md)
integrates current main's default prefetch, preserves snapshot source ownership,
balances only visible rows, proves large contiguous exclusion runs without ID
enumeration and retains exact empty-segment reducers on the index path.

The [consistency-boundary recommendation](consistency-boundary.md) proposes a
shared driver consistency layer across execution paths. A new
Redis server extension is not required by this plan.

The [read checkpoint evidence](validation.md), [raw timing samples](read-benchmark.json)
and [author review](review.md) cover `9bc80c0`. They do not close the remaining
metadata, write/compaction, full feature/platform and benchmark requirements.

The [index lifecycle checkpoint](index-validation.md) records one final build per
private output and zero builds/publications for no-op UPDATE/DELETE, with full
snapshot race and native checks. It does not close the remaining parity work.

The [admission checkpoint](admission-validation.md) verifies gate/full-lineage
waiting before SQL or Arrow execution, cancellation/pin cleanup, one-time input
consumption and atomic IPC late-error behavior. Remaining CI and performance
gaps stay in the active plan.

The [metadata sharing checkpoint](metadata-validation.md) removes unrelated native
value copies and legacy name registries, with independent metadata reclamation.
The [next reference/publication design](row-reference-design.md) records remaining
catalog enumeration, row-reference and independent-writer work.

The [descriptor-owned row-reference checkpoint](row-reference-validation.md)
removes publication descriptor enumeration and aggregate base-reference copies;
independent publication is covered by the [publication checkpoint](publication-validation.md);
bounded bucket construction verified locally.

The [bounded bucket checkpoint](bucket-validation.md) verifies point metadata
reads, ordered field directories, resumable construction and reference-safe
interruption/expiry. Large operations and sustained compaction remain open.

The [compaction queue checkpoint](compaction-queue-validation.md) removes idle
catalog descriptor discovery and verifies conditional queue ownership, prefix
admission, lease recovery and two-table writes without caller retries.

The [Ray compatibility checkpoint](ray-validation.md) verifies shared-generation
worker replay after DML/TRUNCATE/DROP, fresh-plan visibility, retries across an
intervening write and TLS rejection without inherited CA trust: 17 cases passed.
The remaining full feature/platform checks stay open.

The [wide-fetch checkpoint](wide-validation.md) restores positional HASH/Arrow
reads through exact snapshot visibility and preserves the complete wide test's
optimized/fallback, value and lifecycle assertions. Local comparative timings
show selective improvement and approximately unchanged full scans; this does
not close the remaining full feature/platform or performance requirements.

The [managed-import report and ACL checkpoint](import-report-validation.md)
restores complete dry-run/Apply/refresh reports and verifies CLI ownership and
explicit refresh. A Software API principal probe confirms that protocol 12's
`adbc:*` row layout defeats the legacy schema ACL recipe. The
[protocol-13 namespace checkpoint](schema-namespace-validation.md) repairs that
HASH/Search access boundary and makes the expanded Software gate pass; complete
restricted-driver command/pin recipes remain open. The
[source-key checkpoint](source-key-validation.md) restores imported `__key`
identity, including generic/positional/late projections and pinned partitions.
Remaining adoption/sweep/ACL regression families and the broader parity work
stay open.


The [import-failure checkpoint](import-failure-validation.md) reproduces and fixes
lost reports after late private-index failure. Atomic rollback, private-index
reclamation and source ownership are checked for create and refresh. Legacy
adoption/unfinished reservations are rejected without migration; current large
operation resumption and the remaining compatibility families stay open.


The [import-index checkpoint](import-index-validation.md) separates source
membership from private native index policy for HASH/foreign copies. Converted
text values and renamed columns can be indexed; explicit capture indexes and
private IndexColumns can be combined. Existing SQL copies remain independent of
source-index DROP/change; broader performance and compatibility work stays open.


The [managed write checkpoint](import-write-validation.md) preserves import
origin through Arrow replacement and binds provenance through joined DML and
explicit derived/CTE projections. SQL/Arrow private-write compatibility and raw
keyed collision coverage are retained; sweep/ACL, large operations, sustained
performance and full platform/Cluster validation remain open.


The [sweep checkpoint](sweep-validation.md) replaces the empty snapshot sweep
result with non-destructive eligibility preview, bounded reference-aware draining,
confirmed counts and partial errors/resumption. Protocol/CLI lifecycle checks
protect pins, active writers and unregistered objects. Restricted-schema ACL sweep
behavior, the full permission recipes and the broader parity work remain open.

The [durable GC batch checkpoint](gc-batch-validation.md) atomically consumes row
directory entries with their zero-reference row deletions. Fresh collectors keep
progress after interrupted replies without SCAN cursors or replay. It supersedes
the sweep checkpoint's directory-rescan limitation; broader large-operation,
platform and comparative performance requirements remain open.

The [insert batch checkpoint](write-batch-validation.md) stages SQL/Arrow inserts
in bounded atomic row batches with durable outcomes, resolves lost replies by
reading rather than resending, and slices Arrow conversion. It removes the old
cumulative insert ceiling while preserving final publication. Unread-input/process
resume, generic large rewrites, sustained compaction and the full matrix remain
required; this is not completion of the large-operation workstream.

The [rewrite batch checkpoint](rewrite-batch-validation.md) extends bounded
row images to UPDATE replacements, DELETE tombstones and ALTER/rekey copies,
with private-only replacement admission, NULL/provenance preservation and
independent no-op accounting. It supersedes the prior generic UPDATE/DDL row
ceiling limitation; operation/input resume and broader parity work remain open.

The [current Cluster checkpoint](cluster14-validation.md) reruns multi-primary
connectivity, managed copies, partitions and node fences on protocol 14, adding
bounded row-batch outcomes and private-page/shared-metadata recovery assertions.
Promotion and abrupt all-node AOF restart preserve exact pinned/committed/private
state and resume compaction; this does not close RDB or the full platform matrix.

The [RDB checkpoint](rdb-validation.md) separately verifies fresh completed RDBs
with AOF disabled on every shard, then recovers routed sentinels, committed/private
state and compaction across an abrupt three-node restart. The interrupted stream
fails explicitly without replay. This closes that RDB restart evidence gap, not
portable backup import, uninterrupted streaming or the full platform/performance
requirements.

The [import journal checkpoint](import-journal-validation.md) removes the
whole-import retained-capture and Apply candidate ceilings through private
reference-owned capture pages. Input is frozen before destination HASHes are
produced and consumed after acknowledged output. This is groundwork for large
operation progress, not a process-resume interface. The measured 10,000-row
refresh median regressed by 4.9%; import throughput and durable operation/input
resume remain open.

The [remaining import resume design](import-resume-design.md) specifies the
durable plan/input-complete transition, independent job references, exclusive
takeover lease, metadata/index restoration and explicit client continuation still
required. Atomic output/capture consumption is a prerequisite; no public resume
API or process-recovery claim follows from the private journal alone.

The [atomic import output checkpoint](import-output-validation.md) links each
registered output batch, capture consumption and durable outcome in one script.
Incomplete runtime outcomes fail closed. This removes an ambiguous continuation
boundary while the durable job/lease/plan work above remains required.

The [main integration checkpoint](main-merge-validation.md) preserves the newly
merged SQL features under snapshot execution and introduces protocol 15 to reject
unsafe mixed-version writes. Focused SQL/lifecycle tests pass, but the complete
ordinary suite has a [recorded failing inventory](main-merge-failure-inventory.json)
requiring isolation, compatibility fixes and a complete rerun. Earlier protocol-14
Cluster and timing reports do not establish current protocol-15 coverage.

The [ordinary aggregate checkpoint](ordinary-aggregate-validation.md) restores
pinned RESP3 inspection/public-write fixtures and exposes/fixes compact tuple
partition bounds on overlays, preserving exact spill/merge and fallback guards.
Hosted ordinary jobs still fail; the complete ordinary/Cluster/platform matrix
and broader parity requirements remain open.

The [cleanup permission checkpoint](cleanup-permission-validation.md) replaces
legacy mutable-move takeover assumptions with current private-generation cleanup
recovery, checked with both deterministic denial and a real Software API principal.
It also restores the independent unmanaged HASH collision race without bypassing
snapshot admission for driver SQL. Full ordinary/platform and performance parity
remain open.

The [multi-action ALTER checkpoint](alter-multi-validation.md) restores pinned
RESP3 inspection and reconciles fresh private indexes and serialization conflicts
without weakening rejection/atomicity assertions. Nine race cases pass, including
actual private-index creation followed by a lost reply, orphan cleanup, exact
concurrent outcomes, pinned-source preservation and seven deterministic stale
constraint/write conflicts. Broader parity remains open.

The [definition-only ALTER checkpoint](alter-metadata-validation.md) removes row
copies and index builds for eligible definition changes through the existing
private descriptor/publication protocol. Conversion and validation retain their
rewrite path; pinned schema, dependency conflicts and exact command counts are
checked. Ordinary conversion fixtures retain their physical no-rewrite assertions.
Broader lifecycle/platform and sustained performance parity remain open.

The [ALTER interruption checkpoint](alter-interrupt-validation.md) replaces
legacy cancellation/lease triggers with actual acknowledged private batches and
fixes connection shutdown ordering: socket interruption no longer ends operation
tracking before abort/pin cleanup. It preserves outcome diagnostics and bounded
joining, and restores large immutable-page/cleanup overlap coverage. Full
ordinary/platform and sustained performance parity remain open.

The [isolated reader-lease design and model](reader-acl-design.md) validates six
race-enabled cases on verified Software 4x QPF, including actual ACL denials,
cross-principal retention, stale membership and read-only lost-reply resolution.
The production catalog is unchanged and remains protocol 15. This is staged
evidence only: production ACL integration, bounded namespace management and the
ordinary reader-role regression remain incomplete.

The [Arrow partition-lifetime checkpoint](arrow-partition-validation.md) restores
canonical storage/layout validation without exhausting retained generations:
completed live groups are explicitly released, and rejected oversized input must
leave the root unchanged and its target absent. The six-family race selection
passes 72 test events without skips/failures. Full ordinary/platform and
performance parity remain open.

The [SQL cancellation checkpoint](cancel-validation.md) restores the canceled
CTAS fixture's atomic expectations after a real acknowledged private batch.
It checks unchanged publication, absent public metadata, reference-safe cleanup
and name reuse; all fifteen cancellation test events pass under the race detector.
Full ordinary/platform and performance parity remain open.

The [sparse parallel and late-projection checkpoint](parallel-late-validation.md)
restores immutable sparse input construction and enables eligible late projection
after writes create multiple segments. Ordered output limits apply globally and
cap fallback retains complete projections. Exact result and transfer checks are
recorded with their remaining full-parity limits.

The [comment/rekey checkpoint](comment-validation.md) restores pinned metadata
inspection and canonical forced-rekey ownership. Deterministic same-definition
publication races require exact winners and explicit serialization refusals,
with current metadata checked before fresh client continuation. All nine comment
family race cases pass; full parity remains open. Earlier PR description evidence
is preserved in the [history archive](pr-evidence-history.md).

The [constraint/default checkpoint](constraints-defaults-validation.md) restores
scoped rejection/metadata checks, fresh-layout rekey ownership and creation-zone
fixture publication. Default tests retain exact materialization and virtual
planner guards, while stale INSERT publication must conflict without leaking
rows or changing the current root. Both focused race selections pass; full
ordinary and performance parity remain open.

The [timestamp inspection checkpoint](timestamp-inspection-validation.md) resolves
logical IDs and newest stored images under one pin, retaining exact physical
timestamp units, replacement/older-segment/NULL coverage and session-zone
pushdown checks. Seven focused race cases pass; the next ordinary failure is a
DROP/RENAME race receiving serialization refusal. Full parity remains open.

The [DROP/RENAME checkpoint](drop-rename-validation.md) replaces legacy rename
following with exact publication conflicts. Public statement hooks verify name
reuse safety, pin-protected rekey inputs, temporary ownership and reclamation;
13 race-test events pass without skips. Full ordinary/platform and performance
parity remain open.

The [hosted ea20f38 checkpoint](hosted-ea20f38-validation.md) records both Linux
Software Go failures and partial integration passes/skips with a sanitized
function inventory. It also makes the Ray worker private-CA check required in
CI; all seventeen cases pass locally with no skips. The hosted rerun and full
platform/ordinary/performance gates remain open.

The [managed-copy partition checkpoint](partition-copy-ownership-validation.md)
enables native managed foreign copies on the live partition path and lets a
validated capability resolve its captured copy on an ordinary worker without
foreign discovery. Ownership, explicit refresh, old-generation replay and private
reclamation pass Go/native checks; full parity remains open.

The [pruned partition checkpoint](pruned-partition-validation.md) restores
producer-generation lifetime through DML, TRUNCATE, DROP and GC while preserving
predicate pruning, cancellation, explicit release and reclamation coverage.
The focused race selection passes 38 test events; full parity remains open.

The [partition readiness checkpoint](partition-readiness-validation.md) restores
pinned cursor-transition and exact HASH fallback checks, with a separate native
segmented readiness transition preserving newest values, deletions and global
ordering/limits. Nineteen focused race events pass with one topology skip; full
ordinary and Cluster API coverage remain open.

The [retained view checkpoint](retained-view-snapshot-validation.md) scopes bounded
view validation through pinned native metadata and restores publication behavior
through concurrent DML/DDL, view replacement, rekey and source retirement.
Cancellation still aborts private uploads without exposing descriptors; full
ordinary/platform and performance gates remain open.

The [retained capture checkpoint](retained-capture-validation.md) restores
snapshot-aware bounded capture fixtures, privately published exact sparse IDs,
materialized SQL defaults and separate missing-value decoder coverage. Managed
external ownership and retained legacy-producer refusal pass 16 race events;
full ordinary/platform and performance parity remain open.

The [retained execution checkpoint](retained-execution-validation.md) keeps one
statement pin through low-level execution and final definition validation.
Thirty-one race events pass complete Arrow comparisons, temporary-source checks
and resource/side-effect admission; full parity remains open.

The [partitioned aggregate fixture checkpoint](partitioned-aggregate-fixtures-validation.md)
restores pinned cap recovery, privately published sparse worker input and confirmed
live-cursor cancellation. Twenty-three race events pass exact aggregate/top-K
checks against an independent complete serial reference; full parity remains open.

The [public partition fixture checkpoint](public-partition-fixtures-validation.md)
restores retained generation expectations, complete bounded stream replay after
worker closure and privately published sequence boundaries. Test capabilities
are released independently of producer lifetime; full parity remains open.

The [primary-key checkpoint](primary-key-validation.md) restores public rejection
checks, valid optional-key metadata and private publication races. Four race
families pass key lifetime/NULL enforcement and explicit 40001 conflicts, with
fresh revalidation and private reclamation; full parity remains open.

The [rekey Close checkpoint](rekey-close-validation.md) replaces legacy phase hooks
with acknowledged private-copy and committed-reply boundaries. Ten race events
plus a focused registration check pass rollback, lost-reply outcome resolution,
cleanup deadlines and stale-operation fencing; full parity remains open.

The [native rekey layout checkpoint](rekey-layout-validation.md) replaces legacy
name counters and mutable field setup with canonical ownership/freshness and
public DDL. Six race families pass dbt generation retention, complete multi-page
copy, option/temporary behavior and dropped-field reclamation; full parity remains open.

The [native rekey recovery checkpoint](rekey-recovery-validation.md) restores
abandoned-work recovery, publication conflicts, lease fencing and exact concurrent
write outcomes. Sixteen combined rekey/Close race events and a lost-reply GC check
pass. Verified admission backpressure remains a sustained-performance concern;
process-kill, full ordinary/platform and performance gates remain open.

The [native rekey crash checkpoint](rekey-crash-validation.md) kills actual child
processes at acknowledged private-copy and committed-root boundaries. Three
50,000-row recovery cases plus their parent pass under the race detector, with
exact retained values/types, output non-reuse and reader-protected reclamation.
Natural lease-duration, full ordinary/platform and performance gates remain open.

The [native rekey takeover checkpoint](rekey-takeover-validation.md) restores
concurrent private-output collectors, earlier-incarnation name reuse, typed expiry/
serialization diagnostics and DROP recovery. Four race families pass without test
skips. Full ordinary/platform and performance gates remain open.

The [shared-scan checkpoint](shared-scan-validation.md) holds one pin through
planning/execution, compares native segmented visibility with independent exact
rows and an explicit directory reference, and retains separate live-cursor/range
primitives. Seventeen race events pass reader lifetime through DDL/GC, cancellation
and cursor reclamation. Full ordinary/platform and performance gates remain open.

The [ALTER retirement recovery checkpoint](alter-cleanup-recovery-validation.md)
replaces mutable cleanup markers with real SQL field removal, pinned old images
and a lost reply after a bounded retired-row batch. A new connection completes
reclamation without changing current rows or revealing old values on column-name
reuse. Full ordinary/platform and performance gates remain open.

The [native TRUNCATE checkpoint](truncate-validation.md) verifies fresh storage,
complete old table/view readers, identity semantics, unrelated-table preservation
and multi-table publication conflicts without replay. TRUNCATE and the adjacent
schema-cascade race families pass. Full ordinary/platform and performance gates
remain open.

The [native stale-row race checkpoint](stale-row-races-validation.md) exercises
real acknowledged private batches against competing create/DROP/recreation for
SQL, Arrow and owner-local temporary writes. Nine race events pass typed 40001
refusal, exact replacement values, reader retention, input consumption and private
reclamation. Remaining TRUNCATE/UPDATE race fixtures and full parity remain open.

The [statistics checkpoint](statistics-validation.md) fixes an unpinned public
GetStatistics call and resolves native counts through proven Search visibility or
bounded authoritative directories. Eight race events, unit-only packages and a
rebuilt-native C/ADBC test pass; one ACL-principal check explicitly skips. Full
ordinary/platform, restricted-reader and performance gates remain open.

The [public stream lifetime checkpoint](stream-lifetime-validation.md) restores
pinned cursor inspection and adds exact multi-segment public streams through DML,
TRUNCATE, DROP/recreation and GC. Forty-five stream/UNION ALL race events pass,
including consumer Arrow buffers and pin/source reclamation after EOF, release,
cancellation and Close. Full ordinary/platform and performance gates remain open.

The [sweep primitive checkpoint](sweep-primitive-validation.md) isolates an unmanaged
reservation primitive from snapshot-aware inspection and checks ACL setup before
producing fixtures. Two primitive race events pass with one explicit ACL skip;
eleven separate native ownership/collector race events pass. Driver fences remain
unchanged; full ordinary/platform, permission and performance gates remain open.

The [TAG revalidation checkpoint](tag-revalidation-validation.md) publishes unknown
facts privately, preserves conservative exact queries, and validates fresh facts
through public passthrough ALTER while old readers remain immutable. Two race
families pass, including a competing-writer 40001 refusal and reclamation. Full
ordinary/platform and performance gates remain open.

The [temporary lifecycle checkpoint](temp-lifecycle-validation.md) restores pinned
catalog inspection and native storage assertions for temporary tables, isolation,
views, schema removal, Close and expired-owner sweep. Seven race families pass
without test skips/failures. Full ordinary/platform and performance gates remain
open.

The [rounded-run checkpoint](rounded-runs-validation.md) preserves original
single-index repair counters and separately validates exact three-source ordering,
limits and streaming. Two race families pass. Segmented repair work is recorded
explicitly; performance parity and the full ordinary rerun remain open.

The [TIMETZ checkpoint](timetz-validation.md) validates every stored native time
text/NULL under one pin while preserving SQL and Arrow feature assertions. Nine
race test events pass. The preceding continuation passes 24 events with one
explicit server-pause skip; full ordinary and broader parity gates remain open.

The [native TRUNCATE race checkpoint](truncate-races-validation.md) replaces
legacy mutable pipeline/name-counter hooks with acknowledged native SQL/Arrow
batches and private DDL publication conflicts. It covers committed identity
counters, private CTAS visibility, owner-local temporary writes, old readers and
private/reference reclamation. Remaining UPDATE fixtures and the full ordinary
rerun remain open; see the checkpoint for actual validation results.

The [native UPDATE/DELETE/MERGE checkpoint](dml-races-validation.md) replaces
mutable raceDrop/check-age fixtures with acknowledged replacements/tombstones,
competing TRUNCATE/DROP publications, typed 40001 refusal, exact old/fresh rows
and reclamation. Twenty-six race test events pass without test skips/failures.
The complete ordinary/platform and performance gates remain open.

The [application-index reader gate checkpoint](appindex-reader-gate-validation.md)
updates the managed-copy fixture expectation and checks its unchanged principal
setup before importing data. Two managed-copy race families pass; Software's
existing SETUSER gate explicitly skips the reader fixture. Restricted-reader
support remains unproved. The complete ordinary run at 7fee549 stopped at this
fixture after 81 passes/eight skips; it is not a full pass.

The [Linux ARM64 unit checkpoint](linux-unit-validation.md) runs the current
CGO-disabled root-package executable in a network-isolated container: 942 pass
events, 863 explicit skips, zero failures. The timezone-data skip is covered by a
separate focused pass with matching Go data. This is local unit evidence, not
Cluster runtime, native C/ADBC, hosted CI or the full platform matrix.

The [TRUNCATE readiness checkpoint](truncate-readiness-validation.md) corrects an
inactive readiness test hook and adds real private empty-index sealing through
transient states, cancellation, fresh publication and old-reader reclamation.
Twenty race test events pass without test skips/failures. The full ordinary run
at source 21e8509 stopped at that primitive after 680 passes/16 skips; the full
ordinary rerun and broader parity gates remain open.

The [full-suite budget checkpoint](full-suite-budget-validation.md) records the
774-name continuation's ten-minute package timeout after 879 passes/249 skips,
and gives Make/hosted full-package runs a bounded 30-minute clock. Individual
operation/cancellation deadlines remain in force. Complete ordinary and hosted
passes remain unproved.

The [pre-snapshot read comparison](pre-snapshot-read-validation.md) validates four
controlled native runs in both revision orders, all complete results and observed
storage models. The seven read cases remain 2.24x–6.74x slower than the current-main
mutable baseline; no performance parity is claimed. Nullable descending top-K
ordering and broader pin/metadata overhead are concrete next optimization targets.

The [numeric NULL-order checkpoint](numeric-null-order-validation.md) uses pinned
visibility/cardinality/readiness proofs to retain indexed numeric NULLS FIRST
ordering when its candidate set contains no NULLs. Eighteen final focused and
45 broader ordering/NaN race events pass, with unit packages and a versioned native
build. The [native comparison](numeric-null-order-performance.md) measures a
25.1x before/after default descending top-K improvement. Six other queries remain
2.28x–3.83x slower than the mutable baseline. Full ordinary/platform/recovery and
broader parity remain open gates.

The [validation-batch checkpoint](validation-batch-validation.md) reduces the
per-source FT.INFO/directory checks to a bounded per-table primary pipeline,
retaining all readiness/cardinality/prefix checks, no-retry behavior and
statement-local proof/error ownership. Fifty focused Software race events pass
without test skips/failures; 969 unit race events pass with 888 explicit skips.
The full preceding ordinary binary reaches 1,567 passes/263 skips before an
obsolete streaming assumption fails. The corrected fixture preserves explicit
materialization and independently checks optimized streaming plus actual-NULL
fallback. The remaining 143 ordinary families pass in a separate continuation: 259 race
events, five explicit skips, zero failures. The [paired native measurement](validation-batch-performance.md)
validates all four runs and records modest observed median speedups of
1.02x–1.15x; most read overhead remains unexplained. Current full ordinary,
Cluster/failover and broader performance evidence remain open.
Current upstream integration also remains required after the latest main pull.

The [upstream integration checkpoint](upstream-merge-validation.md) combines
36a9cdd with fetched main f4d5b78, including partition layouts and pinned
presence-only column NULL statistics. The final selection passes 127 Software
race events with one existing ACL skip; package units pass 1,032 race events with
895 explicit integration skips; four native acceptance files pass all 113 cases.
The latest Software fixture observes Redis 8.8.0 and verified effective 4x QPF;
prior timings retain their older server labels. Completed native capabilities
are explicitly released; no retention quota is raised. SQL GRANT/REVOKE remains
gated on isolated reader leases and generation-stable authorization, so current
feature parity is still incomplete. Fresh full ordinary/snapshot, broader native
and platform/recovery, current-source comparisons, public resume and sustained
compaction gates remain open.

The [complete local 815c355 checkpoint](full-815-validation.md) records one full
ordinary race command: 1,938 passes, 272 explicit skips, zero failures; root
1,419.357 s. A separate coverage-checked snapshot selection passes 329 events
with 21 deployment/permission/model skips; root 374.706 s. Hosted checks remain
red: release Linux package clocks were still ten minutes, macOS exposes ACL,
read-counter and writer-progress issues, an AMD64 three-shard job reaches the
16-generation limit during concurrent comments, and the ARM64 real-reader grant
fixture fails snapshot initialization permissions. Release commands now use the
existing 30-minute budget; all other failures remain explicit work. No quota,
assertion or ACL recipe is weakened. Matched-source baseline/profiling artifacts
are prepared privately; no unmeasured performance gain is claimed.

[Bounded generation admission](generation-admission.md) now reclaims one eligible
manifest after a proven pre-mutation capacity refusal, then retries reservation
once before SQL/input begins. The regression failed before the fix; 34 related
race test events pass afterward on verified three-shard Software 4x QPF. Live
work/pins, the 16-generation bound, unknown-outcome no-replay behavior and explicit
sweep counts remain intact. This narrows the hosted concurrent-comment failure;
a hosted rerun and broader parity gates remain open.

[Statement-scoped row-read observation](row-read-observation.md) replaces global
server counters for empty-query and row-value no-read assertions. Only the fixed
metadata graph HMGET is exempt; real scan/row-ID positive controls remain.
Both affected race tests now pass on verified Software 4x QPF with no CONFIG skip
or quiet-counter retries. Hosted macOS and actual Cluster API verification remain
open; no production query or ACL behavior changes in this checkpoint.

[Sustained-write diagnostics](sustained-write-validation.md) add a no-retry native
workload across two populated tables with exact RETURNING and complete final-row
verification. The parent completed 64 updates on two 20,000-row tables, but slowly
(147.879 seconds; diagnostic observer probe explicitly recorded). A separate
failing regression proves compaction requested 256 directory entries per source
for each 64-row checkpoint. Reads now respect the smaller batch; 35 related race
test events pass, including 100,001-row compaction. No end-to-end speedup or
full parity is inferred. Fresh matched-source comparisons remain pending.

Fresh both-order [sustained-write comparisons](sustained-write-validation.md#fresh-comparison-with-the-mutable-baseline)
completed on Redis 8.8.0 Software 4x QPF: all four runs validated 40,000 final rows
and every RETURNING value with no retries. Snapshot 32c9d2f pooled median is
1,709.874 ms versus 4.975 ms for mutable f4d5b78; throughput 0.435 versus 190.879
updates/s. This is a large failed performance-parity gate, not a completed
compaction/performance item. The raw reports retain both orders and every sample.
Profile point-write discovery/publication and per-row compaction script overhead
next; their respective contributions remain unproven.

[Point-write readiness profiling](point-write-profile.md) narrows the sustained
write gap: indexed/direct-ID updates both process one row, and newly created
index readiness accounts for 1,401.825 ms of a 1,430.743 ms update with an
unrelated 20,000-row table. With no unrelated table it is 746.081/780.125 ms.
A separately aborted private-index prototype creates an empty index and notifies
one unchanged final row under its writer fence; readiness takes 2.330 ms and
public SQL retains the prior value. Both opt-in race diagnostic cases pass.
Production behavior is unchanged. Next implement and validate bounded,
directory-driven final indexing with complete ownership/identity/completion and
failure/recovery proofs before adopting it; do not substitute the prototype
latency for a production throughput result. Existing full-parity gates stay open.

[Fixture request validation](fixture-request-validation.md) records a stalled
idempotent certificate update before tests, bounded GET/PUT/bootstrap reads, and
skipping an already-installed public certificate. Six setup/exec mock tests and
subsequent actual verified Software setup pass; QPF/noeviction/TLS gates remain.
No test pass is inferred from the interrupted setup invocation.

[Directory-driven final indexing](directory-indexing.md) is implemented for
ordinary private SQL/Arrow outputs: bounded registered-directory traversal,
whole-batch writer/identity/permission admission, unchanged row-ID notifications,
and warning-free indexed-row proof plus existing readiness checks. A typed
query-permission refusal preserves write-only compatibility through a new index
with the original initial-scan proof; transport failures never select it.
Final exact-source checks: 353 snapshot race pass events/23 explicit skips,
1,035 unit pass events/918 expected skips, versioned native build, and 113 native
Arrow/extension/partition/statistics passes with no skips. The old snapshot native
binary is preserved for fresh comparisons; current latency/throughput and full
ordinary/platform/recovery gates are still pending. No full parity claim.

Fresh [six-run directory-index comparisons](directory-indexing.md#fresh-sustained-write-comparison)
complete in both revision orders with all returned/final values checked and no
retries. Median UPDATE improves from 1,712.677 to 31.553 ms (54.28x), but sustained
throughput only improves from 0.433 to 0.714 updates/s (1.65x), and p95 worsens to
7.606 s. Mutable remains 5.474 ms/164.530 updates/s. This is still a failed
performance-parity gate. Attribute admission/maintenance stalls next, including
per-row compaction scripts and repeated full-prefix copies; do not hide them
with larger limits or caller SQL retries. All raw samples and failed-tail result
remain published.

The [atomic compaction copy checkpoint](atomic-compaction-copy.md) replaces
per-row copy mutations with bounded atomic images/checkpoints, preserves partial
output recovery and prevents stale error/checkpoint rewinds. Full snapshot race
and 113 native checks pass. Fresh both-order comparisons improve throughput
1.095x, but remain 219.95x behind the mutable baseline: the sustained parity gate
still fails. Complete ordinary and hosted platform validation are pending.

The [phase profile](compaction-phase-profile.md) measures a 7.38-second full-base
job (3.35 seconds in copy steps, 3.75 seconds in waits); omitting active pacing
still takes 5.50 seconds. [Selective compaction](selective-compaction-design.md)
is a design under evaluation, with tombstone and actual old-worker validation
required before enabling it. The [hosted failure record](atomic-compaction-hosted-validation.md)
keeps ordinary generation pressure and reader authorization failures visible.
The competing-reservation admission regression now reproduces/refines the bounded
pre-execution recovery: 60 repeated race events, the complete snapshot selection
(372 passes / 24 skips), unit-only race checks and 113 native tests pass. The full
ordinary c4698f3 checkpoint passes separately; current hosted validation remains
incomplete. An
explicit sequential verified Software snapshot CI step is staged; reader failure
remains a required final job gate while independent native checks can run.

Upstream main 3aa87e2, including bounded Substrait plan execution
(4fd0894/23860f8), is integrated in the Substrait checkpoint. Its metadata and
execution reader lifetime use snapshot pins; scoped native C-boundary checks
pass. Existing SQL-only checks could not prove the new frontend's consistency.
The upstream supported plan limits remain unchanged.

PR #284 reported CONFLICTING/DIRTY against main before integration. The query
iterator and schema-entry conflicts are resolved while retaining both snapshot
and Substrait behavior. Remote mergeability still requires verification after
publishing; the upstream frontend's mutable-storage tests alone do not prove
snapshot parity.

The [Substrait integration checkpoint](substrait-snapshot-validation.md) resolves
main's query/statement conflicts while retaining snapshot scanners and schema
pins. Targeted upstream and new snapshot lifetime/visibility checks pass, as do
the combined snapshot selection (382 passes / 24 explicit skips), unit race
checks and 118 selected native tests. Current hosted/ordinary/recovery and
sustained performance checks remain open. These changes address the new
frontend without expanding its supported operator/type/parameter/partition scope.

The [selective compaction checkpoint](selective-compaction-validation.md) avoids
repeated large-base copies for small overlays, retains tombstones, and proves
actual older-native handoffs at the hard segment limit. Full-copy resource and
recovery assertions remain exercised with genuinely large source inputs. At
820ccfd the complete snapshot selection passes 406 events / 24 skips, unit race
checks pass 1,097 / 958 expected skips, and complete native validation passes
565 tests plus three subtests / eight skips. A fresh six-process comparison
improves throughput 3.651x, but median latency regresses 1.985x and mutable
throughput remains 61.217x faster. Performance parity fails. A current controlled
phase profile attributes about two seconds per small job to inactive waits;
index readiness polling, index construction and foreground median attribution
remain separate work. Hosted/platform/Cluster/HA and input resume remain open.
