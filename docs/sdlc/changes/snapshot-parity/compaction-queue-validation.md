# Validation: durable dirty-table compaction scheduling

Date: 2026-11-03. Parent checkpoint `989b721`. Status: locally verified;
[full parity](plan.md) remains incomplete. Comparative timing is pending.

## Implementation and scope

Storage protocol 12 adds a due-work sorted set and candidate payload HASH beside
the bounded control catalog. Publication updates eligibility only for changed
table descriptors, reusing the descriptors already checked by final validation.
There is no additional table/schema enumeration or descriptor read to build this
list. Tables with at least four generations are queued; DROP, TRUNCATE and shorter
lineages remove their entries. Queue updates and root publication share one Lua
operation; malformed queue input is checked before publication. Earlier formats,
including 11, are rejected without modification, with no migration or full-catalog
upgrade/reseed scan.

Eligible updates replace the descriptor version but preserve the original queue
age. Discovery examines at most 16 due candidates and pins a catalog only when
there is work. Table identity, lineage and immutable generation-prefix proofs
fence job admission. A newer delta does not invalidate an older captured prefix;
installation preserves its tail. Claiming a job atomically registers source/output
references, writes its durable lease/checkpoint record and consumes the candidate.
Subsequent publications can enqueue further work. Version-aware conditional cleanup
cannot remove a newer candidate after DROP/recreation or another publication.

Failed candidate source checks are deferred for one second using a conditional
update, allowing other due candidates to progress. Timeout cleanup has a separate
bounded context. Existing durable copy/index/install phases, writer admission,
reference ownership and lease takeover remain intact. Discovery no longer scans
every catalog table. Separate orphan-index/row-GC work can still list indexes;
this change does not eliminate every catalog-size-dependent maintenance cost.

## Regressions

- Before: an idle discovery pass read eight unrelated descriptors. After: zero.
  Claiming one queued table also reads none of eight unrelated descriptors.
- Private/aborted staging creates no candidate. Eligible publication enqueues it;
  later metadata updates advance its version without resetting its age. Stale
  cleanup cannot remove the newer record. TRUNCATE clears both queue records.
- Malformed maintenance input is rejected with the catalog root unchanged.
- A consumed work item resumes under a replacement client/worker after lease
  release. Existing full-suite tests also cover copied checkpoints and old-owner
  fencing. This is not a new AOF/failover test.
- A missing index on the oldest candidate is deferred while a healthy candidate
  starts and completes.
- DROP/recreation between queue peek and root acquisition preserves the new
  candidate and cannot start an obsolete job.
- An append between source capture and claim still admits the proven four-source
  prefix; installation retains the newer tail and its exact value.
- Sixty-four alternating UPDATEs on two tables complete without caller retries;
  current values are 32/42 while an older pin retains 0/10.

An initial test setup counted the discarded empty CREATE generation as retained
history. Eligibility tests now use four actual generations. Admission originally
required an exact descriptor version; prefix admission replaces that unnecessary
restriction while retaining table/lineage/version fences against stale cleanup.

## Checks

Owned Software proxy fixture: three primary shards, strict TLS, active verified
4x QPF and noeviction. Mutating suites ran sequentially. Native libraries use
`make -C go build` and its version linker flags on macOS ARM64.

- Complete snapshot race suite: **125 pass events, seven expected Cluster API
  skips, zero failures**, 126.424 seconds.
- All Go unit packages: **895 pass events, 705 expected integration skips, zero
  failures**.
- Native C/ADBC snapshot validation: **12 passed**, 13.345 seconds, including
  repeated appends without caller retries and concurrent independent Arrow input.
- Native IPC module: **nine passed**, 1.17 seconds.
- gofmt and diff whitespace checks passed.

Author self-review checked publication/CAS ordering, queue age, source identity
and prefix proofs, conditional cleanup, failure deferral, durable job takeover,
exact metadata identities and reference accounting. This is not maintainer approval.
Large atomic operations, broader sustained-throughput work, full feature/CI and
platform compatibility, final Cluster API/failover/AOF reruns, comparative evidence
and owned cleanup remain required. No full feature/performance parity is claimed.
