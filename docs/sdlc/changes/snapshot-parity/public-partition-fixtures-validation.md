# Public partition snapshot lifetime fixtures

Date: 2026-11-04. Parent 10327f3. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The fresh 571-name ordinary continuation passes seven test events, then fails
TestPartitionReads because it expects TRUNCATE to invalidate a retained descriptor
(root 1.800 s). The driver correctly admits its original producer generation.

Public partition fixtures now require exact original rows through later INSERT,
TRUNCATE, DROP and GC. A worker's temporary table must not shadow the producer's
permanent source. Forged targets fail without returning readers. Explicit release
must reject new admissions and reclaim private segment HASHes, indexes,
directories and ownership records. Fresh empty descriptors preserve Arrow schema.
A separate live replay case checks repeated old reads after UPDATE/DELETE/INSERT,
while fresh SQL independently sees the changed values.

Streaming fixtures retain bounded Arrow batches, cancellation, release and Close
errors on native directory scans. They do not require a Search cursor for this
path. A fresh worker fully fetches all 25,000 original exact values after another
worker closes, with no batch above 1,000 rows and no segment cursor leak. Real
Search-cursor cancellation is separately covered by the aggregate checkpoint.
Sequence-boundary fixtures publish native metadata privately instead of changing
a pinned sequence key.

The shared live-partition test helper now registers capability release using an
independent cleanup client, so producer closure does not prevent cleanup.
Manually produced incremental groups also register cleanup. This changes fixture
lifetime after a test finishes; production Close still does not revoke handed-out
descriptors.

## Verification and review

Owned Redis Software with effective active 4x QPF and noeviction verified before
testing; mutating selections run sequentially:

- Initial public read/stream/limit/time-zone/cap selection: 12 race events pass,
  zero skips/failures; root 6.732 s.
- Final complete public fixture selection: 14 race events pass, two explicit
  skips, zero failures; root 10.138 s. Skips are protocol ACL management and the
  separately gated Search-configuration mutation case, not compatibility passes.
- Shared-generation/release/expiry/tamper and legacy-refusal selection: nine
  race events pass, no skips/failures; root 3.204 s.
- Formatting and diff whitespace checks pass.

Reviewed original versus fresh generation checks, forged-result cleanup,
producer-independent fixture release, reader ownership, batch bounds and exact
reclamation. No production changes are required. This author review is not
independent maintainer approval. Full ordinary/platform, restricted-reader
integration, current-protocol Cluster recovery, resumable input and
sustained/comparative performance gates remain open.
