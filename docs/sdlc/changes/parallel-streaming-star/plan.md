# Plan

Date: 2026-11-23. Status: local implementation and synthetic validation.
See [intent](intent.md), [specification](spec.md), [verification](verification.md).

1. Add narrow direct-fact admission and exact-directory range/grant setup.
2. Probe read-only prepared buckets in worker-local pipelines; bound fan-in and
   keep the existing reducer single-threaded with deterministic range order.
3. Preserve complete predicates, source failure precedence, cancellation and
   iterator/lease/token cleanup, including queued and one-worker grants.
4. Test source gaps/int64 limits, duplicates/NULLs/provenance, real parsed exact
   decimal/CASE results against the serial path, backpressure and race safety.
5. Integrate and validate only selected backed-up Software queries separately;
   keep full benchmark suites stopped. Do not publish performance claims from
   synthetic tests or the earlier nonidentical native candidate.
