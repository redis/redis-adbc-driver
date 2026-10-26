# Plan and evidence

2026-10-21; baseline dc6f319; #363; see [intent](intent.md), [spec](spec.md).

1. Verify Homebrew's current native Redis includes Search and record provenance.
2. Add owned native-process fixture startup/shutdown and version evidence.
3. Extend the existing macOS release job to full Go and installed-wheel ADBC
   integration; preserve old-tag guards and sequential shared-database checks.
4. Validate on local macOS arm64 with a native Redis/Search process and installed
   wheel; check fixture failure/ownership, workflow syntax and collection gates.
5. Record new evidence and residual gaps, then open a separate PR.

Homebrew/homebrew-core's Formula/r/redis.rb currently builds Redis 8.10.2 with
modules (build-modules commit 3ea813626f0a; inspected 2026-10-21). Older bottles
may lack Search; its presence is an explicit gate. CI records the actual brew
and Redis/module versions. Local fixture binaries are extracted to /tmp, not
installed into the user's Homebrew prefix; no global service is started.
## Verification and blocking defect

Native Redis 8.10.2/Search 8.10.1 ran locally on macOS 26.7.1 arm64 with the
Homebrew default 16 Search workers. Startup/health/version capture and owned
shutdown passed. Port-collision startup and a mismatched-PID shutdown were
rejected while the original fixture remained alive. No global brew prefix,
service, host trust store or paid resource was modified.

The first full Go run exposed missing post-ALTER rows in four existing tests;
10 focused repetitions reproduced it with Go 1.26.6. This became issue #390.
[PR #391](https://github.com/alberttwong/redis-adbc-driver/pull/391) adds the
readiness/cleanup correctness fix and is a prerequisite of this CI change.
No test assertions were relaxed or Search workers disabled. With that fix,
the original four tests passed 20 repetitions and full Go suites passed on
native macOS (main 74.063s, including runtime cap changes) and OSS Cluster
(main 150.815s); focused native/Cluster race checks passed.

Built the arm64 library with Go 1.26.6, MACOSX_DEPLOYMENT_TARGET=12.0 and matching
cgo flags, then built/installed the macosx_12_0_arm64 wheel. The full configured
ADBC suite loaded that installed library on native Redis with real cap 137:
371 passed, 8 existing feature skips, 3 subtests (7.30s). All 9 required partition
cases passed. With ADBC manager 1.8.0, all 9 partition cases passed (1.75s), no
skips. The collection gate passed for both reports. The package lookup used
Python 3.12.15; validation used Python 3.13.16 and PyArrow 26.0.0.
See the [runtime fix's machine-readable evidence](../alter-readiness-390/evidence/macos-wheel-validation.json).

Actionlint 1.7.12 (shellcheck unavailable), Python compile and diff checks
passed. New hosted macOS jobs have not run yet; CI definitions are not results.
The existing release build is reused and old refs without the helper retain
their original load/unit behavior. No extra native rebuild is introduced.

## Remaining gates

This is plaintext local/native Redis validation, not macOS TLS or Cloud/proxy
coverage. Those remain in #363. The target macOS 12 wheel tag retains existing
packaging policy; local runtime checks were on macOS 26.7.1, and hosted runtime
checks target macOS 15. A versioned runtime floor needs separate older-OS
execution evidence. No new release was published.

PR #389's installed-wheel TLS jobs also passed on Linux amd64 and arm64; see
[the updated TLS record](../partition-tls-363/plan.md).
