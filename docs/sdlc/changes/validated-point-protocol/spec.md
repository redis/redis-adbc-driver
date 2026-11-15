# Validated point-read protocol: specification

Date/source/status/requestor: [intent](intent.md). The native proof candidate changes an existing path; the larger upstream API remains a proposal, not a supported driver mode.

The [protocol proposal](../../../validated-point-protocol.md) maps the dependent metadata path to the current source, defines required input/output and authorization semantics, and lists adversarial acceptance cases. It retains protocol 17, live pin ownership, exact native data, fail-closed unknown outcomes, newest-to-oldest lookup and restricted/shared reader behavior.

The opt-in `TestSnapshotPointProtocolProfile` uses production SQL execution on one pre-acquired execution transport for the whole run; per-statement pool acquisition, transport registration and finish/return/drain are excluded. It must fully exhaust/release the Arrow reader and close the statement inside its timer. It records per-query timings and native command/pipeline shapes using the existing privacy-preserving hook. It validates all results, including int64 extrema, values beyond double precision, NULL, empty strings, missing rows and tombstones. A separate HASH HGETALL control validates its own raw values. These are Go diagnostic measurements, not redis-py comparisons or benchmark speedup evidence.

Setup creates a one-segment table, measures three cases, then creates two overlays and verifies exactly three segments before measuring four more cases. Production maintenance stays enabled on its independent pool. An unexpected layout is a failure. The native candidate changes only non-strict missing-image verification: published ZCARD and ZSCORE share a native pipeline, while private owned segments keep scalar ZSCORE. Found-row and strict-membership paths stay intact. Both commands retain typed decoders and NoRetry; count errors/mismatch take precedence, and only exact native Nil with no unaccounted outer error proves absence. Wrapped/joined Nil must fail before reading any older image.

Required future API tests include all selector ACL combinations, permission revocation, corrupted reference/presence proofs, expired/revoked pins, descriptor aliases, undeclared/disallowed discovered keys, oversized descriptors/rows, lost/composed replies, cancellation and slot migration. Shared/restricted readers remain on existing paths until their own equivalent authorization protocol is proven.

Review: [PR #440](https://github.com/redis/redis-adbc-driver/pull/440).
