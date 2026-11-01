# Specification: season-aware TIMETZ expectations

Date: 2026-11-01. Source: `7d6fb15`. Status: implemented; review pending.
Inputs: [intent](intent.md). Verification: [plan](plan.md).

## Functional requirements and implementation

- In `go/timetz_integration_test.go`, expect row 8 for `t = '10:00'`, plus
  row 10 when today's New York offset at 10:00 is UTC-05.
- Apply the same expected IDs to the view queried from a UTC session. Order
  the view results by ID and continue checking both creation-zone literals.
- In `go/timetz_test.go`, evaluate the January timestamp cast against the
  offset-free literal at fixed January and July statement times: true in
  January, false in July.

## Boundaries and risks

This changes tests only. It preserves the current date semantics documented
in [the driver reference](../../../driver-reference.md), SQL execution,
storage, and the eight-environment CI matrix. The integration test still uses
the wall clock; fixed-date unit cases cover both seasonal comparison outcomes.

## Acceptance criteria

The original integration test fails with the two CI mismatches. All TIMETZ
tests pass against Redis Software with active, effective QPF 4x verified.
Formatting and whitespace checks pass. CI evidence is linked when available.
