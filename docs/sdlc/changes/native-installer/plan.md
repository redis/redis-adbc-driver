# Plan: native installer and validation

Date: 2026-10-29. Base: `b1d0613`. Status: local implementation complete.
Inputs: [intent](intent.md), [spec](spec.md). Evidence: [validation](validation.md).

1. Inspect PR #240 checks; create a follow-up branch from its merged main.
2. Build the current native driver. Start an independently named Software fixture
   with verified effective 4x QPF; exercise plaintext queries and both manifests.
3. Configure that owned fixture for verified TLS. Add doctor --ca-file and a
   credential-redacting live harness covering expected connection failures.
4. Implement standalone release download, safe library extraction, stable library
   placement, owned manifests, upgrade and uninstall. Add integrity, ownership,
   rollback and lifecycle unit tests.
5. Smoke-test the published native download and real source-library upgrade,
   discovery/query and uninstall. Wire unit, Linux/macOS lifecycle and Linux
   Software TLS checks into existing PR/release CI.
6. Document commands, ownership, rollback limits and recovery. Record actual
   results and pending remote jobs. Remove the fixtures created for this work.

Review follows REVIEW.md: file ownership, TLS/credential handling, handle cleanup,
archive boundaries, failure recovery and documentation claims. No human review
or release approval is inferred from an agent's checks.
