"""Airflow's Arrow hook adapted to Redis's autocommit-only connections."""

from airflow.providers.apache.arrow.hooks.adbc import AdbcHook

from . import dbapi


def _require_autocommit(autocommit):
    if not autocommit:
        raise dbapi.NotSupportedError(
            "Redis ADBC supports autocommit=True only; transactions are not implemented"
        )


class RedisAdbcHook(AdbcHook):
    """Use an Airflow ``adbc`` connection with a Redis URI in extras.db_kwargs.

    Inherited ``get_records`` and ``get_first`` use ``run`` with autocommit
    enabled. Explicit transaction requests are rejected. For inherited
    ``insert_rows``, pass ``autocommit=True``.
    """

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
