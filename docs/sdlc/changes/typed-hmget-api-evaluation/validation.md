# Validation and negative native disposition

All five final pinned and five current-master API race tests passed with no
skips/failures; master custom vet ./... passed. Driver overlay race checks passed
43 events with one intentional timing-diagnostic skip. These checks used owned
Redis Software with verified active/effective 4x QPF and strict data TLS/RESP3.
No complete upstream suite, independent Cluster or CSC-invalidation claim.

All 36 current-master actual-API parser observations and 56 pinned native trials
validated completely, with no failed/retried/discarded measurements. Parser
elapsed/allocation improvements did not translate to the complete driver:
native median elapsed rose 5.8–8.7%, with 2.8–5.5% less maximum-worker RSS.
The driver dependency and adapter remain unapplied. No retuning followed.

The first full-duplex Software test had an invalid before-next result-reading
hook and reported a race; only the test changed to obey the documented contract.
An unrelated current-master mock callback compilation failure was reproduced on
clean pre-feature master464d41a and repaired only in that test signature. Failed
processes have explicit nonzero exit codes, including compilation failures that
have zero failed-test events. Every raw-log digest and corrected result remains
in the [complete observations](../../../../benchmarks/typed-hmget/2026-11-13/results.json).

Candidate provenance includes clean driver base3213aa54, API backport30e29d1,
actual Go overlay source/JSON hashes and private module/sum hashes. Parser uses
the distinct current-master proposal1f0ca9a. The [report](../../../../benchmarks/typed-hmget/2026-11-13/report.md)
records all identities, excluded costs, ordering/shared-host/RSS limits and
upstream [PR4308](https://github.com/redis/go-redis/pull/4308).

The loaned DB returned empty/no-index and all clients/processes exited. After
all remaining tracks ended, the parent verified final effective QPF/TLS/RESP3 and
DB emptiness, removed the owned container/network and generated environment,
TLS, settings and credential files. All one pre-existing container and 31
networks survived. The [sanitized proof](../../../../benchmarks/typed-hmget/2026-11-13/cleanup.json)
is retained; no raw-log credential redactions were needed, so digests are unchanged.

The all36+56 audit, public-artifact privacy, local links, Python runner syntax
and driver-adapter patch applicability checks passed. The fresh-worktree
reproduction runner itself was not used for another benchmark invocation;
actual measurements used the frozen private controller. No additional Go tests
or timing runs were performed to prepare this evidence.

See [intent](intent.md), [contract](spec.md) and [plan](plan.md).
