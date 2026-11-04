# Specification

Date: 2026-11-04. See [intent](intent.md) and [plan](plan.md).

- `make -C go test-software` starts or checks the owned Compose fixture, verifies
  its RAM topology, noeviction policy, primary readiness, Search endpoint and
  effective active 4x QPF (six Search workers, mnp routing, 32 connections).
- Only after those gates, the runner executes the requested process with the
  owned authenticated TLS URI, CA and retained-control persistence declaration.
  An inherited URI/CA must not redirect that declaration to an arbitrary server.
  The declaration is not persisted in `build/test.env`.
- Wrong topology, disabled TLS, unsafe/unknown policy or ineffective QPF must
  prevent command execution. The runner tests use real jq gates and mocked
  Docker/API responses, isolated in temporary directories.
- Existing driver allkeys/unknown-policy rejection and missing-introspection
  behavior remain intact; no production driver changes are needed.
- Focused retained publication/capture tests must pass on the verified fixture.
  Record the complete Go suite separately, including skips and unrelated errors.

The test workflow runs the runner regression test and triggers when it changes.
No compatibility claim follows from mock tests or skipped capability tests.
