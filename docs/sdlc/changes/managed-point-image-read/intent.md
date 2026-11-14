# Managed single-row image retrieval

Date: November 14, 2026 (America/Los_Angeles). Author: Codex; requestor: repository
user. Base: 0b216478. Status: focused validation complete; integration pending.
[Specification](spec.md), [plan](plan.md), [validation](validation.md).

Managed point writes remain expensive after PR #437. A 1,000-row/four-column
production-worker diagnostic reproduces roughly 40/65/50-ms INSERT/UPDATE/DELETE
medians. UPDATE spends 192 HMGET and 192 HGETALL exchanges plus 320 individual
membership probes across 32 operations, because the same logical key can miss
several newer immutable segments before its old image is found. Reduce those
serial exchanges without changing publication, visibility or ownership.
