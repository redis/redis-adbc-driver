"""Airflow provider metadata, loadable without the optional Airflow dependencies."""

from importlib.metadata import version


def get_provider_info():
    """Expose the Redis ADBC connection without replacing Redis or generic ADBC."""
    return {
        "package-name": "adbc-driver-redis",
        "name": "Redis ADBC",
        "description": "SQL and Arrow connectivity to Redis through the Redis ADBC driver.",
        "versions": [version("adbc-driver-redis")],
        "connection-types": [
            {
                "connection-type": "redis_adbc",
                "hook-class-name": "adbc_driver_redis.airflow.RedisAdbcHook",
            }
        ],
    }
