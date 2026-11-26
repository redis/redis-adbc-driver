# Airflow

The Airflow adapter, examples, installation instructions and tests moved to
[redis-adbc-integrations](https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/blob/main/docs/integrations/airflow.md).

Install the separate `redis-adbc-integrations[airflow]` distribution. Its
version matches the driver release it requires: X.Y.Z requires
`adbc-driver-redis[dbapi]==X.Y.Z.*`, so pip installs the matching driver wheel.
Adapter imports now use `redis_adbc_integrations` instead of `adbc_driver_redis`.
Releases are published to PyPI after each driver release; until the first one
appears there, see the destination README for source installation.
