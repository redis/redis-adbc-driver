# Specification

2026-10-22. Base: `590ab0a`. Status: diagnostic tooling, no driver behavior change.

Run the existing NaN join/text reproduction against a dedicated three-shard
Software fixture with active, effective 4x QPF verified by `make -C go redis-up`.
Build host and Linux test binaries from the same checkout and Go toolchain,
with cgo disabled. Match the Linux binary to the fixture's architecture.

Run the host binary through the published endpoint, then the Linux binary
inside the existing Software container using loopback. Never overlap these
tests: both own the same table names. Reverify fixture configuration before the
Linux run. Preserve each process's exit status, count completed passing/failing
cases, and report incomplete runs. A test failure must make the runner fail
even when the other path passes. EOF-line counts are diagnostic counts, not
counts of unique sockets or failed tests.

Raw test output, fixture configuration output and binaries stay in a new private
directory outside the checkout. The summary contains counts, elapsed time and
runtime versions, without endpoints, credentials, infrastructure identifiers or
SQL text. The runner removes its copied binary but leaves the explicitly named,
caller-owned fixture running. The caller must remove that fixture afterward.

A passing Linux control narrows the investigation; it does not prove a Docker
defect, identify the termination initiator, or establish general Linux or
three-shard compatibility. This diagnostic does not replace full integration
and native validation.

Provide a separate read-only [client-only reproduction](../../../../scripts/software_eof_client.go)
that imports go-redis without importing the ADBC driver. It opens fresh clients,
requires confirmed RESP3, disables command retries, checks PING and the nil HGET
reply from a synthetic absent key, and closes every client. It emits only counts,
error categories, runtime information and elapsed time. Run only after verifying
the same dedicated Software/QPF fixture. Fail on any failed or incomplete
iteration, bound the run by five minutes, and reject nonlocal/credentialed URIs.

See [intent](intent.md), [plan](plan.md), [evidence](evidence.md).
