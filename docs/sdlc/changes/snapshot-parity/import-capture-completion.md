# Private import capture completion

Status: private statement prerequisite for [operation resume](import-resume-design.md),
not a public resume API. The full [parity specification](spec.md) remains active.

## Intent and contract

Import output previously relied on its Go caller reaching the end of capture.
A future owner must instead distinguish a complete journal from a client that
stopped midway through discovering input. Persist that distinction before output
can consume captured rows. Do not restart external discovery, reevaluate values,
or turn an unknown reply into replay.

The importer begins private capture before calling the source iterator. After
the source reports successful completion, it closes the journal and validates
membership in bounded steps. Each step checks at most 128 keys, targeting 1 MiB
of source-key bytes; the existing single-entry ceiling remains 64 MiB. A page of
wide keys is never fetched as one Lua array. Validation checks exact HASH/ZSET
cardinality, every member's zero score and corresponding payload presence/size.
Existing output decoding retains typed payload validation and exact native values.

The checkpoint contains counts, a phase and the private output identity. It stores
no Redis SCAN cursor or source-key string, preserving arbitrary binary source
keys without JSON encoding. Once validation starts, appending input is refused,
including after a failed validation. A completed journal remains immutable.

Only a ready capture can emit import images. Plain batched insertion and ordinary
row mutations cannot bypass capture consumption for its output generation.
Each import batch atomically commits its complete images, directory registration,
input consumption, remaining count and durable batch outcome. Runtime-interrupted
output retains the existing abort-only marker and the prior remaining count.
Sealing/publication requires complete validation and zero remaining input.
Empty captures retain their existing empty-table behavior.

Unknown completion replies fail the operation without resending the transition
or invoking the source again. The original durable record can be inspected
read-only; this implementation does not detach it or automatically publish it.

## Ownership and compatibility boundaries

The marker is bounded extra state inside an ordinary private workspace. Its
ownership, base/output references, lease expiry, abort and reclamation are
unchanged. Published rows and manifests keep their protocol-16 formats; successful
publication replaces the workspace with the existing compact committed record.
Old collectors understand all workspace ownership states used here. This adds
no independent retention, new collector state, migration or disable switch.

Detached ownership still requires the separate protocol revision described in
the resume design. The marker alone does not preserve a job after ordinary
statement abort/expiry, retain its terminal plan/outcome, restore metadata
dependencies, or bind allocated row IDs across process takeover. SQL and Arrow
capture/resume, owner fencing, final indexing recovery and native operation
handles all remain required. A second Go journal handle under the same private
lease is not cross-process resume evidence.

No ACL key grant is broadened. The scripts use commands already in the canonical
trusted-writer profile. SQL readers do not gain private-workspace mutation.
External HASHes/indexes remain application-owned; their observed values are
copied and subsequent application changes require explicit refresh. Autocommit,
Active-Active exclusion and noeviction requirements remain unchanged.

## Implementation and validation plan

The capture transition lives in `snapshot_import_capture.go`; normal import
capture/consumption, generic mutation admission and seal/publication use its
server-side proof. The completion loop advances only after an acknowledged
checkpoint; errors abort without replay. Empty, partial and malformed captures
must remain collectible through the existing metadata graph.

Required checks include early output/publication refusal, closed-journal
immutability, exact binary keys and integers, bounded narrow/wide validation,
unknown replies, existing atomic consumption/failure tests, external ownership,
large imports, and private capture persistence across real promotion and RDB
recovery. Persistence proof must remain separate from process-resume claims.
Run Software with verified effective 4x QPF and keep operational state private.

## Completed evidence

- Against unmodified candidate `9036c90`, the focused negative Cluster fixture
  demonstrates that the old scripts admit output, seal and publication despite a
  synthetic incomplete-capture record. This tests a new prerequisite; ordinary
  old import callers already waited for their source and no public resume API
  existed. It is not evidence of partial imports through those old callers.
- The new Cluster cases pass output/plain-output/raw-HSET refusal, seal/publication
  refusal, a 513-row validation checkpoint at exactly 128 members, exact integer
  and binary-key consumption, empty input, equal-count mismatched membership,
  failed-check immutability, wide-key byte budgeting and a lost completion reply
  with exactly one transition call. Source discovery/output is not replayed.
- The first positive fixture omitted managed import provenance and was correctly
  refused at publication. Supplying its canonical import identity fixes the
  fixture; the production provenance check remains intact.
- All unit packages pass, root 26.898 seconds; integration skips in this run are
  not deployment proof. The actual Cluster fixture is three nodes, three physical
  primaries and three replicas, strict TLS, noeviction and verified effective
  QPF4, database Redis 8.6.2 on Software `8.2.0-78.18`.
- Existing import/adoption/provenance race regressions pass, root 114.674 seconds,
  on the separate owned Software 8.8/TLS/QPF4 fixture. This includes large capture,
  split output, source preservation, native types, atomic input consumption and
  failure cases. Exact event/skip counts are recorded after final inspection.
- Native C/ADBC snapshot validation on that fixture: 19 passed, no skips,
  13.191 seconds, versioned library built with `make build`.

Broader snapshot races and expanded promotion/RDB capture checks are pending.
These are correctness observations, not matched performance benchmarks. No
performance-parity, public-resume or complete-goal claim follows from this stage.
