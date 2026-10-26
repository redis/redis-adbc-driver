# Plan and evidence

2026-10-21; baseline dc6f319; #390; see [intent](intent.md), [spec](spec.md).

1. Preserve the native Redis/Search regression before changing runtime code.
2. Await backfill before ALTER metadata publication; preserve errors/cancellation
   and orphan attribute behavior, including TRUNCATE catch-up retries.
3. Avoid index inputs during dropped-field cleanup, preserving global operators
   and aggregate semantics. Use allocated HASH pages so base ACLs need no SCAN.
4. Test controlled readiness/commit/cancel transitions, terminal errors, pending
   cleanup inputs/aggregates and deferred/direct partition behavior.
5. Run existing ALTER/default/row comparison regressions unchanged, full native
   and OSS Cluster suites, race checks and installed macOS wheel validation.
6. Open a separate fix PR; the macOS CI change follows it.

## Reproduction

Local macOS 26.7.1 arm64; extracted official Homebrew Redis 8.10.2_1 and OpenSSL
4.0.3 bottles in /tmp (no prefix install/service). Search 8.10.1, default 16
Search workers; persistence disabled; dedicated localhost:17779.

Initial full Go suite failed four existing tests: TestSQLAlterMultiMixed,
TestSQLAddColumnDefault, TestSQLAlterColumnDefault, TestSQLRowValueKeysetBound.
Repeated unchanged cases with Go 1.26.6 (-count=10) reproduced missing rows.
Raw redis-py RESP3 also returned 997-999 of 1000 inserted HASH rows after
FT.ALTER while FT.INFO reported indexing=1 and incomplete percent_indexed,
without any Go driver. This is a reproduced unsafe backfill interval, not a
claim that all potential backend omissions have this cause.

The readiness barrier alone fixed most cases; a remaining all-NULL row case
in TestSQLAlterMultiMixed overlapped background HDEL cleanup. HASH fallback
while PendingCleanup is recorded fixed that case. The original four unchanged
cases then passed 20 repetitions. First full run caught NOPERM SCAN in the
existing TestACLDriverUser; replaced managed cleanup SCAN with bounded allocated
HASH pages. The unchanged base-ACL regression then passed, without ACL changes.

## Verification

- The four original unchanged regressions passed 20 repetitions after the fix.
- Full native macOS Redis 8.10.2/Search 8.10.1 Go suite with real runtime-cap
  discovery enabled: all three packages passed (main 74.063s; CLI 0.908s;
  benchproxy 0.168s). No native Search workers disabled (default 16).
- Full three-primary OSS Cluster Redis 8.6.2 Go suite: all three packages passed
  (main 150.815s; CLI 1.395s; benchproxy 0.243s).
- Focused native and Cluster race checks passed, including controlled readiness,
  cancellation before metadata commit, unused attribute retry, pending cleanup
  scans/aggregates, and partition/direct behavior. Page-size, global sort/limit,
  sparse/deleted rows, cancellation and stale-prefix coverage were expanded.
- Built the macOS arm64 library with Go 1.26.6 and deployment target 12.0;
  built/installed its native macosx_12_0_arm64 wheel. On native Redis, actual
  cap 137: full C/Python ADBC validation 371 passed, 8 existing feature skips,
  3 subtests (7.30s); all 9 required partition cases passed. Minimum-supported
  ADBC manager 1.8.0: all 9 native partition cases passed (1.75s), no skips.
  See [credential-free machine-readable evidence](evidence/macos-wheel-validation.json).
- Format/diff checks passed. Software/proxy jobs and the new PR's hosted checks
  have not yet run; local native success is not Cloud/proxy compatibility.

No Search workers disabled, assertions weakened, new SCAN permissions granted,
fixed query-result retry sleeps added, or paid resources provisioned. ALTER can
now wait for backfill in proportion to index work. Pending cleanup HASH reads
can be slower on sparse allocated IDs and normal blocking SQL still has its
ordinary materialization costs. This is not a shared transactional snapshot.
