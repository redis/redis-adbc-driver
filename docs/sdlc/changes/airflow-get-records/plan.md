# Plan: Airflow get_records

Date: 2026-10-29. Source: `95d6583`. Status: local implementation complete.
[Intent](intent.md), [specification](spec.md), [evidence](validation.md).

## Ordered work and files

1. Inspect upstream hook and reproduce its transaction rejection using the
   current native library against disposable Redis Software.
2. Add `python/adbc_driver_redis/airflow.py`, optional dependencies and focused
   tests, retaining upstream Arrow result handling and cleanup.
3. Document explicit hook usage and limits; configure a separate CI environment.
4. Run focused integration and adjacent installation/autocommit tests; review
   correctness, options, TLS forwarding, cleanup and documentation claims.

## Release and remaining work

Local tests passed. Configured Linux CI was not executed here. Airflow 2.x and
other provider versions were not tested. No publication or human approval is
recorded.
