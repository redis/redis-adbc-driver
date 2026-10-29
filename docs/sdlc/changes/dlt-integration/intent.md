# Intent: dlt integration

Date: 2026-10-29. Source: `95d6583`. Requestor: user in this Codex chat.
Author: Codex. Status: implemented, local verification complete.
[Specification](spec.md), [plan](plan.md), [verification](validation.md).

Provide a tested dlt integration using the Redis ADBC driver. Let existing dlt
sources load SQL tables over native Arrow ingestion without a Redis protocol
client or SQLAlchemy adapter. Deliver optional dependencies, an executable
example, integration tests and CI coverage.

Scope is an append-only custom destination. Native driver behavior, transactional
writes, replace/merge, remote pipeline state and automatic migrations are excluded.
The user subsequently requested a pull request; publication is out of scope.
