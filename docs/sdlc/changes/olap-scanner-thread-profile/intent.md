# Intent

Date: 2026-11-24. Public baseline: 95c0d9ef. Status: ready for review.
See [specification](spec.md), [plan/evidence](plan.md).

Allow an authorized third-path OLAP run to request independent ADBC partition
readers through a compatible custom scanner, retaining released-scanner default
compatibility and sanitized requested-only provenance. No live query or full
benchmark suite starts through this change.
