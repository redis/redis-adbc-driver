# Dependency refresh and code reduction

Date: 2026-11-17. Base: `b63a010d`. Requestor: repository user. Author: Codex.
Status: implemented and locally verified; submitted for PR review.

## Problem and outcome

The user requested a check for newer dependencies, compatible updates, and
opportunities to reduce custom code. Refresh the active Go driver, Python
integration requirements, pgdiff lockfile, AWS provider lock, and matching client validation pins.
Use standard library facilities where their semantics match existing code.

## Scope

Preserve SQL semantics, Arrow ownership, cancellation, TLS and storage protocols.
Preserve historical benchmark/source pins and compatibility bounds with documented
reasons. No deployment, release, ClickBench run or unsupported server testing.

See [specification](spec.md) and [implementation/evidence](plan.md).
