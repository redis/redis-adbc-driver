# Specification: resolve uncertain heartbeat outcomes with server proofs

Date: 2026-11-18. Source: `06302c39`. Status: implemented and locally verified; no maintainer approval recorded.
Issue: [#490](https://github.com/redis/redis-adbc-driver/issues/490).
Inputs: [intent](intent.md). Verification: [plan](plan.md).

## Functional requirements and implementation

- Keep the two-minute server lease and 30-second heartbeat cadence.
- Preserve each control command's no-replay marker and 10-second attempt context.
- A direct native refusal or zero validation/renewal result terminates the pin,
  preserving materialized-reader expiry diagnostics and native error context.
- Treat transport, wrapped and joined errors as uncertain. Do not release or
  permanently invalidate a potentially live pin solely because its reply was lost.
- On the next scheduled heartbeat, require a positive server `valid` result for
  the same token/root before issuing a new `renew`. Repeated uncertain proofs
  cannot permit renewal. Ordinary successful heartbeats still issue one command.
- Foreground validation remains mandatory. An unavailable proof fails the read;
  a missing or expired server lease cannot be recreated by this recovery path.
- Keep cancellation, one-shot successful release and failed-release retry behavior.

## Boundaries and risks

The implementation changes only heartbeat scheduling/error interpretation in
`go/snapshot.go`; Lua fencing and SQL/Arrow execution are unchanged. After an
uncertain reply the heartbeat may remain active until cancellation/release or a
confirmed refusal. This retains a potentially live pin instead of prematurely
retiring it; every foreground access still proves server ownership. Existing
private-workspace fences continue to reject expired writers independently.

## Measurable checks

Deterministic ticks cover lost/late replies, wrapped errors, proof-before-renew,
expired pins, unavailable proofs, native refusal and cancellation. Software tests
exercise an unmanaged count under the affected metadata pin with renewal replies
lost both before execution and after successful execution. Run existing pin-release,
control-script and unmanaged aggregate tests, plus the unit suite.
