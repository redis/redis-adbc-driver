"""Airflow's Arrow hook adapted to Redis's autocommit-only connections."""

from functools import cached_property

try:
    from airflow.providers.apache.arrow.hooks.adbc import AdbcHook
except ModuleNotFoundError as exc:
    if exc.name not in {"airflow.providers.apache", "airflow.providers.apache.arrow", "pyarrow"}:
        raise
    from airflow.exceptions import AirflowOptionalProviderFeatureException

    raise AirflowOptionalProviderFeatureException(
        "Install adbc-driver-redis[airflow] to use the Redis ADBC provider."
    ) from exc

from . import dbapi


def _require_autocommit(autocommit):
    if not autocommit:
        raise dbapi.NotSupportedError(
            "Redis ADBC supports autocommit=True only; transactions are not implemented"
        )


class RedisAdbcHook(AdbcHook):
    """Use a ``redis_adbc`` connection with a Redis URI in extras.db_kwargs.

    Inherited ``get_records`` and ``get_first`` use ``run`` with autocommit
    enabled. Explicit transaction requests are rejected. For inherited
    ``insert_rows``, pass ``autocommit=True``.
    """

    conn_type = "redis_adbc"
    default_conn_name = "redis_adbc_default"
    hook_name = "Redis ADBC"
    # Keep adbc_conn_id for existing explicit hook users and provider discovery.
    conn_name_attr = "adbc_conn_id"

    @classmethod
    def get_ui_field_behaviour(cls):
        return {
            "hidden_fields": ["host", "port", "login", "password", "schema"],
            "relabeling": {},
            "placeholders": {
                "extra": '{"db_kwargs": {"uri": "rediss://user:password@localhost:6379/0"}}',
            },
        }

    @cached_property
    def dialect_name(self):
        return self.connection_extra_lower.get("dialect", "redis")

    def get_conn(self):
        options = dict(self.db_kwargs)
        uri = options.pop("uri")
        return dbapi.connect(
            uri,
            db_kwargs=options,
            conn_kwargs=self.conn_kwargs,
            entrypoint=self.entrypoint,
        )

    def set_autocommit(self, conn, autocommit):
        _require_autocommit(autocommit)
        super().set_autocommit(conn, True)

    def run(
        self,
        sql,
        autocommit=True,
        parameters=None,
        handler=None,
        split_statements=False,
        return_last=True,
    ):
        """Execute using autocommit, including calls from inherited fetch helpers."""
        _require_autocommit(autocommit)
        return super().run(
            sql,
            autocommit=True,
            parameters=parameters,
            handler=handler,
            split_statements=split_statements,
            return_last=return_last,
        )
