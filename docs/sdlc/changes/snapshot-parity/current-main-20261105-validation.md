# Current upstream reconciliation

Date: 2026-11-05. Snapshot parent: c54a6cd (production 7c30fa6).
Incoming main: cc2cbb2. Merge: e03255a. Subsequent payload ACL fix: a48ccc9.
Status: unit/focused Software and full versioned native checks pass;
full combined ordinary/snapshot/platform checks remain required.
See [intent](intent.md), [spec](spec.md), [plan](plan.md) and
[point-lease evidence](point-lease-validation.md).

Main adds materialized-view reader lifetimes, analytics/usage instrumentation,
CI selection/wheel checks and a separate opt-in Flex stopgap. Its 17 conflicts
overlap snapshot catalog planning, HASH scans, storage, connection/transport,
rekey fixtures, aggregate readers and workflow budgets. Preserve both sets of
fields/dispatch where applicable rather than replacing snapshot behavior with
the old mutable storage lifecycle.

Managed materialized views retain statement pins and immutable reference-owned
generations. They do not register the mutable layout's second reader lease or
retirement HASH. Legacy collectors explicitly refuse a canonical managed row
prefix; only snapshot reclamation may delete those generations. The new real
fixture clones native strict-TLS/RESP3 client options without managed hooks,
registers one erroneous legacy retirement record, requires a typed refusal, and
checks the actual directory/index and exact BIGINT result remain intact. Its
first setup attempt was caught by the managed client's metadata-write fence;
the corrected unhooked legacy client avoids that false premise.

Upstream view cases now inspect actual managed ownership. They retain complete
25,000-row old-reader validation across a shrinking refresh, multiple pinned
generations, release/cancel/Close reclamation, renewal, abandoned readers,
publication races and DROP/recreate fencing. Open-recovery cases stop earlier
workers and only poll: the newly opened connection's automatic collector must
reclaim the abandoned generation. They do not invoke a test collector to make
that assertion pass. The restricted-reader assertion remains; it is not made a
compatibility pass through broader grants.

The upstream materialized-reader expiry contract is retained: Invalid State,
SQLSTATE 55000. A real post-release/Open recovery case first reproduces the
generic snapshot IO error instead (1.07 s). A statement-local, mutex-protected
materialized-reader marker selects the existing view expiry error after a
confirmed absent/expired pin, including a definite reader-only renewal refusal. Transport
and cancellation errors retain their own classification. No storage format,
lease lifetime or input replay behavior changes.

Flex reader fields and the upstream range reader remain integrated, but managed
Flex source execution is refused before dialing because its index/publication
contract is still unverified. No live Flex test is counted here. This remains an
open parity requirement rather than a new claimed capability. Reader-only
retained replay stays separate. Snapshot source hooks and all-new-node Cluster
hooks are retained. SQL GRANT remains gated pending the separate production
reader-isolation work; required hosted assertions are not disabled.

The first combined unit/race pass, with live Redis endpoints unset, completes
1,156 test events / 1,003 expected integration/profile skips / no failures,
root 51.477 s. These skips do not prove server compatibility. A first live
view/control/schema/usage race selection passes 32 events / six explicit skips,
root 30.239 s, before the final view-expiry refinement and additional collector
case. The corrected unhooked collector case passes separately (0.20 s) while
the restored expiry assertion reproduces the expected old error mismatch.
After the expiry fix, the complete neighboring view/control selection passes
31 events / six explicit skips / no failures, root 29.917 s. The operator and
ACL fixtures absent from these selections remain explicit skips, not passes.

All live local checks use owned Redis Software 8.8.0, strict verified TLS,
noeviction, active/effective 4x QPF and three primaries behind its proxy;
macOS ARM64 / Go 1.27.3. Telemetry is disabled for development/tests. No local
fixture credential, license, infrastructure identifier or operational artifact
is published. This does not prove Cluster API, failover or persistence recovery.

At a48ccc9, the full versioned native ADBC suite passes 565 tests and three
subtests, eight explicit capability skips, 86.18 s; Python 3.14.10 / PyArrow
26.0.1 / manager 1.13.0. Library SHA-256:
`df7b2928615b9bb6570b6a1466cc29645fd5afbc6bf392e852e3daf2dffb22c4`.

The reviewed coordinated fix originally at 2e9769e (cherry-picked as a48ccc9)
replaces broad metadata read access with positive metadata key-family patterns.
Its regression uses the production import capture path, verifies owner decoding,
and denies restricted raw HGET/HMGET/HGETALL of captured rows. The coordinated
author reproduces those reads before the fix and verifies denial afterward on
a separate owned verified Software fixture. The SQL reader Open/EVAL gate is
still open at this stage; this does not claim production reader isolation.
Independent RBAC/snapshot unit races on the combined source pass 38 test events,
five expected server/role skips, root 1.901 s. No server claim follows from skips.

Full combined ordinary/snapshot/platform checks, comparative measurements and
reader/Cluster/resume/utility gates remain required before full feature and
performance parity can be claimed.
