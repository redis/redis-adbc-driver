# Predeclared contract and acceptance

No production behavior or release script changes. The new opt-in test mode drives
seven exclusive dense jobs in this fixed order: control-before, same-client PING,
separate-client TIME, pure 20 ms pacing, separate-client PING, same-client TIME,
control-after.
Every job recreates the existing 20K-row/four-version target and unrelated table.
Fetch and validate all target typed values; separately check unrelated COUNT/MIN/MAX.
Controls bracket interventions; they are not repeated native throughput candidates.

Before any execution, parent source review added the pure-pacing arm because the
old five-millisecond worker pacing and INFO wait-shift observations do not separate
request work from simply allowing time to pass. No new timing result motivated
this amendment. The pacing arm uses a cancel-aware 20 ms timer, no Redis command,
zero probe I/O, and no probe/release connection relationship. Its elapsed is outside
the original release clock and remains inside full job time and the original
release deadline. A common prelude duration permits comparing prelude plus release.

A probe runs immediately before the original release. Capture its complete elapsed
separately, then retain the original release-hook/socket clock and full job clock.
No INFO or other counter observation occurs inside either command. Probe failures
are recorded and never replace the real release return value or directly request
another release. Each probe has an independent 250 ms context; its elapsed still
consumes part of the already-created release deadline. Retain and report that
pacing/deadline limitation rather than claim a transparent observer.
Validate observer completeness after ordinary cleanup and full data validation.

Read/write counters are above TLS and count plaintext calls/bytes, not TCP packets.
Dial-local ordinal IDs establish actual same/different connections; never assume
pool reuse. Only fixed modes, durations, counters and non-secret local ordinals
leave diagnostics. Record strict verified TLS. The portable net.Conn API has no read-only
TCP_NODELAY getter, so explicitly mark that flag unobserved rather than add
platform-specific code or change socket options. Do not change TLS, RESP3, QPF,
server scheduling, host networking or production proof/lease/cleanup operations.

Acceptance: all seven jobs complete exact result checks and normal releases, one
record per actual release, all requested probe connection relationships observed (pacing is not applicable),
probe replies validated and zero pool misses/timeouts during releases. Invalid
observations fail only the diagnostic assertion after complete cleanup/results;
retain every failed attempt. Compare probe+release and whole-job distributions,
not just a shifted subspan. A command/connection/pacing effect is not proof of
server CPU, queueing, TLS processing or a TCP mechanism. No immediately fresh
INFO counters or native packet/server trace is assumed available. No production
optimization follows without a separately justified correctness/performance gate.

See [intent](intent.md) and [plan](plan.md).
