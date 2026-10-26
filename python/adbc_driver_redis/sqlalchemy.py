"""PostgreSQL SQL compilation/catalog reflection over Redis's ADBC DB-API.

Analytics only: SQLAlchemy transaction scopes cannot undo Redis writes.
"""

from sqlalchemy import exc
from sqlalchemy.dialects.postgresql.base import PGDialect


class RedisDialect(PGDialect):
    name = "redis"
    driver = "adbc"
    default_paramstyle = "qmark"
    supports_statement_cache = True
    supports_native_decimal = True
    supports_native_uuid = False
    supports_sane_rowcount = False
    supports_sane_multi_rowcount = False
    supports_multivalues_insert = True
    supports_sequences = False
    insert_returning = False
    update_returning = False
    delete_returning = False
    use_insertmanyvalues = False
    _backslash_escapes = False

    @classmethod
    def import_dbapi(cls):
        from . import dbapi

        return dbapi

    # SQLAlchemy 1.4 (used by Superset 6) calls dbapi; 2.x calls import_dbapi.
    dbapi = import_dbapi

    def create_connect_args(self, url):
        # URL.render_as_string handles percent-encoding and bracketed IPv6.
        scheme = "rediss" if url.drivername.split("+")[0] == "rediss" else "redis"
        uri = url.set(drivername=scheme).render_as_string(hide_password=False)
        return [], {"uri": uri, "autocommit": True}

    def initialize(self, connection):
        # pg_catalog here implements PostgreSQL 16 contracts, independently of
        # the Redis server version. Redis GetInfo is not a PostgreSQL version.
        self.server_version_info = (16, 0)
        raw = connection.connection
        self.redis_server_version = raw.adbc_get_info().get("vendor_version")
        self.default_schema_name = raw.adbc_connection.get_option(
            "adbc.connection.db_schema"
        )
        self.default_isolation_level = "AUTOCOMMIT"
        self._supports_smallserial = False
        self._supports_drop_index_concurrently = False
        self._supports_create_index_concurrently = False

    def get_isolation_level(self, dbapi_connection):
        return "AUTOCOMMIT"

    def get_isolation_level_values(self, dbapi_connection):
        return ["AUTOCOMMIT"]

    def set_isolation_level(self, dbapi_connection, level):
        if level.replace("_", " ").upper() != "AUTOCOMMIT":
            raise exc.ArgumentError("Redis ADBC supports AUTOCOMMIT isolation only")

    def do_begin(self, dbapi_connection):
        pass

    def do_commit(self, dbapi_connection):
        pass

    def do_rollback(self, dbapi_connection):
        pass

    def do_savepoint(self, connection, name):
        raise exc.InvalidRequestError("Redis ADBC does not support savepoints")

    def do_begin_twophase(self, connection, xid):
        raise exc.InvalidRequestError(
            "Redis ADBC does not support two-phase transactions"
        )

    def get_indexes(self, connection, table_name, schema=None, **kw):
        # PGDialect 1.4 expects int2vector's space-separated text cast. Redis's
        # catalog exposes an integer array, so use the column positions directly.
        import json

        from sqlalchemy import text

        oid = self.get_table_oid(connection, table_name, schema, **kw)
        rows = connection.execute(
            text("""
            SELECT i.relname, ix.indisunique, ix.indkey, a.attname, a.attnum
            FROM pg_catalog.pg_index ix
            JOIN pg_catalog.pg_class i ON i.oid = ix.indexrelid
            JOIN pg_catalog.pg_attribute a ON a.attrelid = ix.indrelid
                AND a.attnum = ANY(ix.indkey)
            WHERE ix.indrelid = :oid AND NOT ix.indisprimary
            ORDER BY i.relname, a.attnum
        """),
            {"oid": oid},
        )
        indexes = {}
        for name, unique, keys, column, position in rows:
            keys = json.loads(keys) if isinstance(keys, str) else keys
            entry = indexes.setdefault(
                name, {"name": name, "unique": unique, "columns": {}}
            )
            entry["columns"][list(keys).index(position)] = column
        return [
            {
                "name": entry["name"],
                "unique": entry["unique"],
                "column_names": [v for _, v in sorted(entry["columns"].items())],
                "dialect_options": {},
            }
            for entry in indexes.values()
        ]
