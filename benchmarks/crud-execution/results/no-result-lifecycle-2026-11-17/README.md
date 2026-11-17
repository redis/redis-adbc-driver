# Explicit no-result native correctness follow-up

This is new test-only evidence for #447, separate from the frozen timing results
in `../2026-11-17`. No performance window or GC-policy experiment ran.

Driver source: `beec721216e6c4c3ca448fe162534209b7a7f34e`, verified clean in the
library's Go build metadata. Built with `make -C go build adbc-cli
VERSION=v0.1.0-dev.no-result-lifecycle`. The [native result](native-correctness.json)
records actual library/harness SHA256, packages, client route and server settings.

Actual environment: native macOS arm64/Python 3.14.10 through a Docker published
localhost trusted-TLS port; Redis Software Redis 8.8.0, one shard, active/effective
4x QPF, six Search workers, mnp/32, noeviction. Setup and each authoritative gate
used the owned Software management API. This is not Linux performance evidence.

- Six paired permission cases (12 refused executions): INSERT/UPDATE/DELETE,
  managed/unmanaged, ordinary execute versus explicit executemany. Actual
  independently probed source HSET/DEL restrictions; identical status/SQLSTATE,
  complete typed table state and raw unmanaged HASH/key-set preservation.
  Both paths currently report these refusals as IO (10), with no SQLSTATE. This
  proves parity, not a new Unauthorized/42501 classification guarantee.
- Four cancellation cases: block a foreign parameter producer before writes,
  cancel once, let the producer return, join execution. All return CANCELLED
  (11)/57014, consume the producer once, preserve exact state and permit reuse.
- Four successful-mutation/Close cases: counts 1/0, complete retained Arrow
  values after cursor/connection Close, idempotent public Close, independent
  fresh-connection state and an explicitly acknowledged baseline restoration.
- Exact int64 beyond 2^53, SQL NULLs and an explicitly constructed NUL/UTF-8
  string remain valid. Temporary principal resources are independently absent
  after cleanup; owned schema, application source keys and index are removed.

Earlier attempts remain in private logs. The first stopped at the post-RBAC
QPF gate while BDB reconfiguration was settling; native cases did not run past
that gate. The harness now waits for the same strict effective configuration.
The second exposed Software's `ERR ACL failure in script: No permissions` form
in addition to direct NOPERM, already recognized by the repository's RBAC tests.
The harness recognizes those explicit permission forms, retaining raw principal
refusals, exact status/SQLSTATE parity and complete-state assertions. A subsequent
run passed the permission/cancellation groups; the final run adds the Close
cases, indexing readiness, metadata, independent resource cleanup and sanitized
failure handling represented in the linked result.

This does not qualify concurrent Close during execution, a foreign callback that
never returns, cancellation after uncertain source writes, all nested Arrow
lifetimes, ASAN/UBSAN or retained Go/C leak accounting. The #446 Arrow dependency
blocker and #447's controlled performance/lifecycle acceptance remain open.
