# Plan

Date: 2026-10-22. Author: Codex. Status: design delivered; implementation pending.
Refs [intent](intent.md), [spec](spec.md), [evidence](evidence.md).

1. Record automatic migration and shared numeric identities as the direction.
   Keep the incompatible opaque prototype unpublished.
2. Review numeric allocation, lazy defaults, ALTER checks and partition bounds;
   document the proposed ledger, region ownership and migration state machine.
3. Review durability/fencing and select a supported ledger backend. Define
   Software CRDB discovery, transfer and stable profile interfaces before coding.
4. Implement one new common format and allocator, with no legacy adapter. Version
   the breaking change and reject old/mixed formats before mutation. Add durable
   reservation/recovery tests before regional writers.
5. Implement read-only preflight and a resumable fenced migration controller;
   preserve HASH keys/IDs and exact default/index semantics.
6. Qualify new-format ordinary and multi-region deployments, injection at every
   transition, restores/clones and allocation exhaustion. Verify Software 4x QPF
   on the ordinary source and every instance of a multi-cluster Software CRDB.
   Historical Cloud Pro evidence is not acceptance. Record failures and skips.
7. Publish a runnable guide only after the workflow passes acceptance. Do not
   substitute design examples or opaque-key tests for migration validation.
