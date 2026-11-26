# Dashboard exclusion filters

2026-11-26 · Requestor: Albert Wong · Source: d59bb97f (on the merge of PR #721) · Status: implemented and deployed; not yet reviewed

The analytics dashboard could only include matches. Its *Reporting traffic*
selector picked one traffic category, and table search, column filters and the
execution-sample filter kept only rows containing the typed text. The requestor
asked to exclude both traffic and text, and to deploy the change to the live
console.

Arbitrary traffic combinations were considered and declined. Each filter is a
precomputed summary in one object the dashboard API reads per request, so all
31 combinations would need about five times the refresh work and a new storage
format. The requestor chose two fixed presets instead.

Out of scope: arbitrary traffic combinations, server-side text filtering,
Terraform changes and legacy-state reconciliation (PR #723).

See [specification](spec.md) and [plan and validation](plan.md).
