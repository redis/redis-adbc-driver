# Intent: reduce repeated CI work

Date: 2026-11-05 (America/Los_Angeles). Requestor: Albert Wong. Author: Codex.
Source: `a9f3e41`. Status: implemented locally; awaiting PR CI and review.

Reduce redundant PR builds and tests, retain full release coverage, and let
Linux wheel-client checks proceed independently of macOS runner availability.
Retire the completed integer SIMD experiment workflow while preserving its
results, prototype and manual reproduction commands.

See [spec](spec.md) and [plan and validation](plan.md). The PR is the review record.
