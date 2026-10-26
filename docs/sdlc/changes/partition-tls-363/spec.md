# Specification

2026-10-21; #363; see [intent](intent.md), [plan](plan.md).

Use a disposable pinned Redis 8.6.2 TLS-only endpoint, fresh local CA/server
certificates with localhost SANs, client certificate verification enabled and
actual aggregate cap 137. Workers authenticate independently with read-only
commands and metadata/fixture-row key scopes; CONFIG and writes are denied.
No skip_verify, existing trust-store modification, shared production services,
published credentials or paid infrastructure.

Verify complete Arrow values/schema for broad Search reads and selective HASH
reads after planner closure. Include NULL, decimal, binary, missing/deleted IDs,
live writes, cancellation, stale descriptors, malformed input and denied worker
permissions. Descriptors must contain no URI or test credentials. Validate
server certificate trust and hostname failure rather than accepting any TLS
connection. Record versions, checks and local-only deployment limitations.

Linux amd64/arm64 PR jobs use the already-built library; release jobs use their
already-installed native wheel, without duplicating driver builds. TLS failures
are required failures. Cleanup is bounded and only removes fixture resources.
MacOS packaged/TLS and Cloud/proxy TLS coverage remain open in #363.
