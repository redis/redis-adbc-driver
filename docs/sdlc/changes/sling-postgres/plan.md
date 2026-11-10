# Plan

Date: 2026-11-10. Source: `85509485dbc07b2e2e0ea9e1b8e08bd11a290587`.
Status: complete. [Intent](intent.md), [specification](spec.md), [validation](validation.md).

1. Inspect repository invariants and upstream Sling connector/SQL templates.
2. Build the library and verify the Software fixture using `make -C go redis-up`.
3. Exercise stock transfers and narrow failures to configuration/consumer cases.
4. Add `scripts/test_sling_postgres.py` and `scripts/sling/adbc.yaml` for a
   repeatable test with cleanup, isolated configuration and private logs.
5. Run the complete harness, compare full values independently in PostgreSQL,
   and document the working path and limitations in `docs/integrations/sling.md`.
6. Per the user's follow-ups, publish the interoperability result upstream and
   add Sling to the README, guide index, installation/support inventories and
   standalone-runner registry. Verify inventory contracts and local links without
   rerunning unchanged live transfers.
7. Fix PR helper CI by adding Sling's standalone workflow disposition to the
   acceptance audit and updating its exact inventory count; reproduce the
   failure and run the acceptance-runner contracts before pushing.

8. Repair PR #371 against current main: retain workflow grouping, place Sling in
   ETL / ELT & integration, reconcile counts, and verify inventory/site contracts.

No driver changes or upstream Sling patch was needed for the configured path.
