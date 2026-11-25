Target: redis/redis-adbc-driver — new issue
Title: Sling: re-validate on v1.6.8 (slingdata-io/sling-cli#836 fixes) and retire template/precreation workarounds
Labels: partner integration, documentation
Status: actionable only after Sling v1.6.8 is released (latest is v1.6.7 as of 2026-11-24). It could be filed now as a reminder.
---
## Upstream status

https://github.com/slingdata-io/sling-cli/issues/836 was closed on 2026-11-22. The maintainer says all three reported gaps are fixed for the next release, v1.6.8:

1. A generic `type: adbc` connection no longer needs a template override. Without a metadata query, Sling uses ADBC GetObjects.
2. The row reader skips zero-row batches, so empty Arrow results no longer panic.
3. Reads keep `text`, `varchar(n)` and decimal precision. A PostgreSQL target still writes decimals as unconstrained `numeric`.

The maintainer tested with the DuckDB and SQLite ADBC drivers, not Redis. As of 2026-11-24 the latest release is v1.6.7, so v1.6.8 is not out yet.

## Workarounds documented today

- `docs/integrations/sling.md:5`, `:123` and `:700`: tested/install version 1.6.6.
- `docs/integrations/sling.md:144-166` and `:741-745`: isolated Sling home plus the `scripts/sling/adbc.yaml` template override.
- `docs/integrations/sling.md:199-204`, `:246-247` and `:747`: precreate the table and use `truncate` with `direct_insert` to avoid the empty-batch path; empty-source transfers are unvalidated.
- `docs/integrations/sling.md:767-786`: limitations list (template, `current_catalog`, the panic, DDL inference).
- `docs/python-integrations.md:226-228`: a summary repeating these limits.

## Proposed work (after v1.6.8 ships)

1. Re-run `scripts/test_sling_postgres.py` (PostgreSQL → Redis → PostgreSQL) with v1.6.8 on a verified Software fixture, using a stock generic ADBC connection without the template.
2. Add the cases the workarounds avoided:
   - default staging without precreation;
   - an empty-source transfer;
   - the `SELECT label FROM demo.inbound WHERE 1=0` reproducer (`docs/integrations/sling.md:775`);
   - the DDL type round trip.
3. Remove or narrow the template and precreation guidance based on what passes. Keep any remaining limitation (e.g. decimal precision on PostgreSQL targets) with its actual observed output.
4. Comment on slingdata-io/sling-cli#836 only if something still fails.

## Acceptance

The guide matches v1.6.8 behaviour measured against Redis, with the existing exact row/NULL checks. Any workaround that remains has a stated, linked reason.
