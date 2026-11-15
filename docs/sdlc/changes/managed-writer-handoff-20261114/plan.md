# Managed writer handoff: implementation and verification plan

Date/source/status: [intent](intent.md); contract: [specification](spec.md). Candidate implemented; root design review approved.

1. Review existing connection lifecycle, snapshot pin cleanup, server admission and the rejected target-progress pacing experiment. Complete.
2. Review the exact opaque identity, bounded registry, cancellation and fallback design with the root agent. Complete.
3. Implement a scoped candidate and focused race/unit coverage for FIFO/handoff, target/principal isolation, independent DB handles, cancellation/Close, unknown native outcomes, bounded overflow and no retained references. Do not build while another task owns timing.
4. Run focused units and verified Redis Software active/effective 4x QPF lifecycle/admission/unknown-input tests in an exclusive root-granted slot.
5. Freeze both persistent binaries and source/harness hashes before interleaved 1/4/8-writer comparison. Retain all warmups/trials, complete typed final-state validation, throughput, p50/p95/max and foreground/maintenance command counts. No SQL/input retries.
6. Require observer-free/native Python/ADBC confirmation with the normal transport lifecycle and direct-client controls before selecting runtime changes. Archive a negative result and restore runtime if the candidate fails selection gates.
7. Record actual server/resources/settings, source/binary identities, skipped checks and owned cleanup without public infrastructure identifiers/credentials. Independent source/evidence review precedes PR integration; human approval is not inferred.

No checks have run for this candidate. PR: integration pending.
