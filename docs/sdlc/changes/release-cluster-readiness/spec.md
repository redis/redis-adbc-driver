# Specification

Date: 2026-10-20. Source: `11faf538`. Status: implementation.
[Intent](intent.md), [plan](plan.md), [validation](validation.md).

The three-primary OSS fixture is healthy only when each configured primary
reports cluster_state:ok and accepts FT._LIST. A reachable seed alone is
insufficient. A failed command must return a failing health status. Accept CRLF
from Redis CLUSTER INFO. Keep Compose's existing bounded healthcheck timeout and
retry count. Tests remain unchanged and run sequentially.

Release failures must preserve Compose logs and per-primary topology/Search
information, including failure during the Software-to-OSS transition. Readiness
checks are read-only; fixture mutation for fault injection is restricted to a
new disposable local container on ports 27001–27003. Existing release tags/assets
are not rewritten. No driver storage, SQL, ACL, or transaction contract changes.
