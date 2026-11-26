# Prefect

The Prefect adapter, examples, installation instructions and tests moved to
[redis-adbc-integrations](https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/blob/main/docs/integrations/prefect.md).

Install the separate `redis-adbc-integrations[prefect]` distribution. Its
version matches the driver release it requires: X.Y.Z requires
`adbc-driver-redis[dbapi]==X.Y.Z.*`, so pip installs the matching driver wheel.
Adapter imports now use `redis_adbc_integrations` instead of `adbc_driver_redis`.
Releases are published to PyPI after each driver release, starting with
0.0.31:

```sh
python -m pip install --only-binary adbc-driver-redis 'redis-adbc-integrations[prefect]'
```
