# Measurement contract

Date: 2026-11-12. Status: complete for the final user-authorized scope. See [intent](intent.md) and [plan](plan.md).

Use the exact v0.0.25 release source and verify its ADBC GetInfo version. Use a
dedicated Redis Software database with active, effective 4x QPF: six Search
workers, mnp routing, and 32 connections. Verify before timing.

Reuse the independently typed 100K x 105 fixture from the prior refresh. Adapt
legacy harness assumptions to public snapshot SQL/Arrow operations. Never mutate
published managed HASHes to construct native baselines. Native HSET uses a
separate immutable fixture with the same public values and typed output. Expose
the difference between native writes and managed snapshot publication.

Complete fetch and compare every typed value and multiplicity outside timing.
Arrow field nullability declarations are schema metadata; assert no unexpected
null values and normalize the declaration before comparing identical types and
values. Retain actual errors and omit unvalidated timings.

Report repetitions, warmups, CPU/RAM, server/tool versions, TLS, timer boundaries,
medians and observed ranges. Do not attribute cross-environment changes solely
to the driver version. Full-scale tests require a verified Software-compatible
backup in the current storage format and correct results before speed claims.

See [completed validation and review](validation.md).
