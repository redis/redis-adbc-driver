# Validation evidence

Date: 2026-11-04. Base: f1e1f03. macOS ARM64, Apple M5 Max, Go 1.27.2.
Status: work in progress. See [plan](plan.md).

`make -C go redis-up` completed: Redis Software 8.6.2, three shards, Search;
the setup's 4x QPF gate completed before tests. Private test.env and operational
artifacts are excluded from commits.

## #306

- `env -u REDIS_URI go test -count=1 -p 1 ./...`: passed before adding the
  recursive SEARCH path and new helper-specific tests; integration skipped.
- `env -u REDIS_URI go test -count=1 -run TestSortSlice -bench BenchmarkSortSlice -benchtime=100ms .`:
  passed. Active comparator cancellation/deadline abort within 256 subsequent
  comparisons; stable ties equal standard-library output; unrelated panic passes
  through. These helper tests do not establish connection reuse or native C behavior.
- Short benchmark, tied integer input: 10k rows standard 1.441 ms, cooperative
  1.526 ms; 100k rows standard 20.830 ms, cooperative 22.649 ms. Roughly 6–9%
  overhead in this sample; no claim for other key types, sizes or hardware.
- Complete Redis integration suite: running; result pending.

## Focused native acceptance

The rebuilt shared library passed 41 selected driver-manager/PyArrow cases,
with 76 unrelated cases deselected. This includes all decimal widths, Date64,
explicit timestamp zones, every duration unit, fixed/large/view buffers, both
partition modes, replay after producer closure and source truncation, nested
STRUCT extension Bind/BindStream/create/append/IPC, dictionary file output,
NULL/default/NaN statistics and streaming DISTINCT. Additional failure and ACL
cases are being run; the complete suite result is pending.

Native validation required task-local uv installation. Python package fetching
uses system roots while Redis uses an explicit test CA. Operational files, URIs,
credentials and raw failure logs remain private.

## DISTINCT performance and complete validation

`ARROW_ACCEPTANCE_PROFILE=1 go test -count=1 -run
'TestSQLCancelInsideSort|TestRequiredKeyStreaming|TestDistinctStreaming' -v .`
passed, including late errors after a cursor page, early release, context,
statement and connection cancellation, retained consumer ownership, clearing
DISTINCT state and zero aggregate worker grants. Three alternating trials per
mode cover 128, 8,192 and 32,768 input rows and cardinalities 1, 17 and all rows.
Every output value and schema is independently validated after timing.

Medians for 32,768 rows (ms; allocations and sampled heap in MiB):

| Cardinality | Mode | First | Full | Allocated | Sampled heap |
|---|---|---:|---:|---:|---:|
| 1 | streaming | 272.41 | 272.48 | 125.87 | 39.42 |
| 1 | ordinary | 170.35 | 170.43 | 119.14 | 42.80 |
| 17 | streaming | 258.04 | 258.10 | 125.87 | 45.13 |
| 17 | ordinary | 175.10 | 175.17 | 119.16 | 42.86 |
| 32,768 | streaming | 126.22 | 280.22 | 133.65 | 50.11 |
| 32,768 | ordinary | 192.34 | 192.41 | 152.53 | 49.67 |

This keyed-source profile includes parsing/preparation and retained consumer
batches. The batch size is 64; low-cardinality input must finish before filling
one batch. No general speedup, peak RSS or constant-memory claim follows.
Active-sort statement cancellation passed for projection, set, window and array
aggregate sorts: observed latency approximately 25–91 microseconds in the focused
run. This is local client latency on these inputs, not a guaranteed wall-time
bound or remote Redis termination.

Final sort-helper benchmark after preserving the original stable/unstable sort
families: 10k standard 1.296 ms versus cooperative 1.496 ms; 100k standard
20.220 ms versus cooperative 22.399 ms. This short sample shows approximately
11–15% overhead for tied integer keys, with roughly 96 additional bytes per sort.
It supersedes the earlier development sample above.

## Complete Go suite

`REDIS_RETAINED_CONTROL_PERSISTENCE=guaranteed go test -count=1 -p 1 -json ./...`
passed on the verified Software fixture: 1,756 pass events and 45 skip events
(including package/subtest events), zero failures. Skips include protocol ACL
management blocked by Software, explicit profiling opt-ins and unavailable
optional topology and server-command checks. The managed-API independent ACL run below covers
Software's separate credential path. An earlier run exposed obsolete assertions
that all layouts reject; those now verify full replay values/schemas instead.

The final dictionary-entry work/remapping refinement additionally passed all IPC,
extension/bounds, descriptor, function-registry and sort-focused unit regressions,
including compact staging with repeated large dictionaries exhausting payload work.

## Complete native driver-manager suite

`uv run pytest -q --tb=short tests/` passed on the final shared library:
**531 passed, 8 skipped, 3 subtests passed**, zero failures, 26.65 seconds.
The matrix includes negative/zero/positive decimal scales and wrong-type/arity
checks across ordinary, live and retained paths. Existing source-free live
queries still reject their shape; retained and table-backed explicit layouts
have positive schema/value coverage. Skips are reported by the suite rather
than treated as evidence for unavailable capabilities.

Four Software fixture unit tests and six candidate TLS-runner unit tests passed.
Python compilation and `git diff --check` passed.

## Independently authenticated managed Software TLS acceptance

The owned three-shard fixture was re-verified with effective 4x QPF, six Search
workers, mnp routing and 32 connections, a verified management CA, TLS-only data
proxy and cap 137. The management-API principal runner passed **18 overall,
5 metadata and 5 retained checks**. Live layouts replay in a separate process
with restricted source ACLs. Authoritative NULL counts pass with Search denied;
separate SCAN, HEXISTS and source-key denials fail instead of returning zero.
Retained explicit layouts replay after producer closure/source drop with a
principal limited to PING/TIME/GETRANGE; namespace denial, early release,
cancellation, revoke and owned-namespace cleanup all pass. Principals and their
role mappings are deleted by the runner. Raw reports and certificate/credential
material remain private in the task's temporary directory.

Final review retained the ordinary stable/unstable sort families, bounded repeated
dictionary visitation independently of compact staging, and rejected pagination
subqueries before incremental planning can materialize them. Focused regression
checks cover these final refinements. No release/package/topology performance
claim or human review approval is inferred.
