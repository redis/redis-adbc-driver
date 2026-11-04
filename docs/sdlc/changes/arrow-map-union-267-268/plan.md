# Implementation and verification

See [intent](intent.md) and [spec](spec.md).

1. Add interned Map/Union schemas and metadata round trips; implement recursive
   HASH encoding, native Arrow extraction/building, and exact append admission.
2. Extend normalization budgets and reject unsafe casts/functions/field access.
   Admit live partitions; explicitly reject retained execution containing these
   types without extending the restricted retained IPC decoder.
3. Extend the shared Go/C layout inventory with native and nested Map/Union
   fixtures. Exercise independent expected schemas/values, Bind/BindStream,
   empty/nonzero slices, IPC stream/file, fresh metadata, replay and ownership.
4. Cover malformed tags/offsets, null keys, schema evolution and decoded budgets;
   run focused Redis Software tests after verified active 4x QPF and the unit
   suite, review the final diff, then submit a PR closing both issues.

## Evidence

Local verification on 2026-11-04 (implementation rebased onto `79efb60`,
which changes the Software suite runner/docs and has no Go driver changes):

- `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...`: passed all
  packages; Redis-dependent tests skip in this unit-only run.
- `cd go && env -u REDIS_URI go test -race -count=1 -p 1 -run
  'TestMapUnion|TestArrowLayout(Inventory|CopiedValues)' .`: passed.
- `make -C go build`: passed, with the Makefile version linker flag.
- Dedicated `make -C go redis-up`, followed by `software-setup.sh --exec`
  verification before each live suite: Redis Software, Redis 8.6.2, three
  shards, Search, authenticated TLS, active verified 4x QPF and noeviction.
- `software-setup.sh --exec go test -count=1 -v -p 1 -run
  'TestArrow|TestMapUnion|TestStoredStruct|TestCursorCancelBoundsCleanup|TestStreamCancel|TestStreamRelease'
  ./...`: 22 top-level tests passed; `TestArrowKeyStreamingProfile` skipped
  because its explicit profiling opt-in was not enabled. Other packages had
  no matching tests. Complete schema/value comparisons finished, including
  independent live replay, ordinary query IPC stream/file, output held across
  cancellation, SQL equality/distinct, and retained NotImplemented admission.
  Retained rejection returns no descriptor or manifest and frees payload quota;
  zero-byte revocation tombstones retain their existing bounded lease semantics.
- Native C tests through the driver manager, PyArrow 26.0.1:
  `python -m pytest validation/tests/test_arrow_interop.py
  validation/tests/test_ingest.py validation/tests/test_statement.py
  validation/tests/test_arrow_acceptance.py -q -rs`: 174 passed, 3 skipped.
  The 43 interoperability cases include direct/streaming ingestion,
  empty/nonzero slices, nested Map/Union, incompatible append, malformed
  tags/offsets/nullable key schemas, and NULL-key imports that must leave the
  driver usable. Existing generic ingestion, statements and Arrow acceptance
  cover the C adapter's effects beyond the new types. Two existing ingestion
  capability cases report `not implemented`; transaction toggling skips because
  the driver is autocommit-only. No skip represents Map/Union acceptance evidence.
- `git diff --check`: passed.

## Review and limits

Self-review followed the correctness/lifecycle, ownership and intent/evidence
passes in [REVIEW.md](../../../../REVIEW.md). No remaining actionable finding;
this records an author self-review, not human maintainer approval. In particular,
Map item nullability is canonicalized to true consistently across Go and C (the
Arrow Go C importer normalizes it), while Union child nullability is preserved.
C preflight checks reject Map constructor hazards and invalid Union headers
before importing, and the stream adapter owns/releases producer callbacks.

The full Redis Software suite and other topologies were not run for this change.
No throughput, process-memory bound or additional topology claim is made.
Retained partitions explicitly reject these types; no restricted retained IPC
decoder admission was added. The C Data ABI carries pointers without allocation
sizes, so producers must still provide readable buffers for declared extents.
Operational identifiers, credentials, logs and generated certificates stay out
of git. [PR #309](https://github.com/redis/redis-adbc-driver/pull/309) closes
#267 and #268 on merge. GitHub Actions remained queued without assigned runners
when inspected, including an earlier repository PR. Main has no branch protection
or active ruleset requirements. Merge uses the completed local acceptance above;
remote CI is pending and is not counted as passed.
