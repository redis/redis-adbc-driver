# Implementation plan

Date: 2026-10-29. Source: `95d6583`. Status: complete.
[Intent](intent.md), [specification](spec.md), [verification](validation.md).

1. Inspect driver ingestion, Python integration conventions and dlt 1.31's
   documented custom destination and installed implementation.
2. Add the append-only destination, optional extra and runnable example.
3. Test actual pipelines on local Redis Software with unique disposable schemas;
   exercise error handling and checkpoint recovery, then run the Python suite.
4. Add Redis Software CI coverage for both architectures; existing OSS analytics
   jobs also include the dlt tests through the test extra.
5. Document setup, supported behavior, operational limits and actual evidence.

No native driver changes or release steps are needed. CI execution and human
review remain outside this local implementation's verification evidence. The PR
branch was subsequently rebased onto main at `8d2cf8f`; dependency floors and
the newer documentation structure were preserved.

The PR destination was corrected to `redis/redis-adbc-driver` at the user's
request. Applied only the integration change onto its main at `71625e1`;
preserved the public repository's Airflow, Prefect, Ray and Dagster updates.
