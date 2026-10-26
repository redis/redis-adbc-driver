# Spec

2026-10-21. Base `1282e4a`. Author: Codex; requestor: Albert Wong (runtime features required). Status: in progress. Refs #359/#360/#362/#363.

Use an explicitly configured tenant namespace, random job identity, and Redis key ACL authorization on every operation. A descriptor is not a bearer credential. Put all tenant quota/job keys in one hash slot so quota reservation, publication and revocation can be atomic. The same independently authenticated reader must be allowed to read the configured namespace/job keys; readonly workers need no EVAL/write privileges.

Reserve each job's output allowance before upload; bound tenant bytes, live/tombstoned jobs, job bytes, chunk bytes and chunk count. Provisional state/chunks have TTL from their first write. Expired quota reservations are pruned on the next quota operation. Publish a checksummed immutable manifest only after every chunk exists, with a fixed absolute server-clock expiry shared by manifest/chunks. Planner closure does not revoke results. Explicit revoke atomically marks the job revoked and removes only that job's chunks/manifest, retaining a bounded expiry tombstone. Account quota is logical retained payload bytes, not Redis allocator/replica overhead or a process RSS limit.

Read-only operations distinguish expiry, revocation, missing/evicted state and checksum corruption; never rerun SQL. Recheck state before each chunk. Validate namespace/job IDs and manifest size/count/schema metadata, never accept arbitrary keys from descriptors. Keep RESP3/TLS/Cluster client defaults unchanged. Public runtime mode remains disabled until the budgeted coordinator and complete Arrow/lifecycle tests are integrated.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
