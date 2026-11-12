# Parallel CI suites intent

Date: 2026-11-12. Base: `53b76248`. Requestor: Albert Wong. Author: Codex.
Status: implemented and checked locally; hosted execution pending.

A completed Test workflow took approximately 40 minutes because ordinary Go
checks, snapshot race checks and Python integrations ran serially on each runner.
The request is to shorten elapsed CI time while retaining the selected
architectures, suite coverage and required Software fixture verification.

Separate the three groups onto independent hosted jobs/databases. Keep shared
Redis packages sequential, integration caches intact, and all checks mandatory.
Do not change runtime behavior, test selection, server editions or test assertions.

See [specification](spec.md) and [plan and verification](plan.md).
