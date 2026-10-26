"""Optional Superset plugin. Install inside the Superset environment."""

from typing import ClassVar

from sqlglot.dialects.dialect import Dialects
from superset.db_engine_specs.postgres import PostgresBaseEngineSpec
from superset.sql.parse import SQLGLOT_DIALECTS

# Superset 6 resolves parsing by backend name, not an engine-spec attribute.
SQLGLOT_DIALECTS.update(redis=Dialects.POSTGRES, rediss=Dialects.POSTGRES)


class RedisEngineSpec(PostgresBaseEngineSpec):
    engine = "redis"
    engine_name = "Redis (ADBC, autocommit only)"
    drivers: ClassVar[dict[str, str]] = {"adbc": "ADBC"}
    default_driver = "adbc"
    sqlalchemy_uri_placeholder = "redis+adbc://localhost:6379/0"
    supports_dynamic_schema = False
    allows_sql_comments = True
    sqlglot_dialect = "postgres"


class RedisTLSEngineSpec(RedisEngineSpec):
    engine = "rediss"
    sqlalchemy_uri_placeholder = "rediss+adbc://localhost:6379/0"
