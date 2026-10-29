# Implementation and verification

See [intent](intent.md) and [specification](spec.md).

1. Build the native library from the current checkout.
2. Add `python/tests/test_pandas.py` with temporary table cleanup and independent
   assertions for native ADBC and SQLAlchemy reads.
3. Run the pandas tests with the existing SQLAlchemy tests against local Redis
   Software; record [results](validation.md).

All steps completed. The user subsequently requested a PR and explicitly
approved publishing to `redis/redis-adbc-driver`. No release or merge is requested.
