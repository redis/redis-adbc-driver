"""PostgreSQL SQL compilation/catalog reflection over Redis's ADBC DB-API.

Analytics only: SQLAlchemy transaction scopes cannot undo Redis writes.
"""

from sqlalchemy import cast, exc
from sqlalchemy.dialects.postgresql.base import PGCompiler, PGDialect, PGTypeCompiler
from decimal import Decimal

from sqlalchemy.engine import default
from sqlalchemy.sql import compiler, sqltypes


def _float_value(value):
    return None if value is None else float(value)


class RedisDDLCompiler(compiler.DDLCompiler):
    """Integer keys require explicit values; Redis has no SERIAL sequences."""

    def get_column_specification(self, column, **kwargs):
        if column.identity is not None:
            raise exc.CompileError("Redis ADBC does not support identity columns")
        return super().get_column_specification(column, **kwargs)


class RedisFloat(sqltypes.Float):
    def bind_processor(self, dialect):
        # Decimal-preserving NUMERIC support does not make Arrow DOUBLE accept
        # Decimal Python objects. Coerce only parameters for floating columns.
        return _float_value


class RedisTypeCompiler(PGTypeCompiler):
    def visit_UUID(self, type_, **kw):
        return "CHAR(32)"


class RedisNumeric(sqltypes.Numeric):
    def bind_processor(self, dialect):
        return lambda value: None if value is None else Decimal(str(value))

    def result_processor(self, dialect, coltype):
        if self.asdecimal:
            return lambda value: None if value is None else Decimal(str(value))
        return _float_value


class RedisCompiler(PGCompiler):
    def visit_truediv_binary(self, binary, operator, **kw):
        if isinstance(binary.left.type, sqltypes.Numeric) and not isinstance(binary.left.type, sqltypes.Float):
            # Coerce the numerator to its declared precision. PostgreSQL's
            # default denominator cast uses bare NUMERIC for NULLIF/functions;
            # Redis's default NUMERIC(38,9) can overflow or round it to zero.
            return "%s / (%s)" % (
                self.process(cast(binary.left, binary.left.type), **kw),
                self.process(binary.right, **kw),
            )
        return super().visit_truediv_binary(binary, operator, **kw)


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
    supports_identity_columns = False
    supports_native_enum = False
    supports_native_boolean = True
    supports_constraint_comments = False
    postfetch_lastrowid = False
    preexecute_autoincrement_sequences = False
    ddl_compiler = RedisDDLCompiler
    statement_compiler = RedisCompiler
    type_compiler_cls = RedisTypeCompiler
    type_compiler = RedisTypeCompiler  # SQLAlchemy 1.4 (Superset)
    colspecs = {**PGDialect.colspecs, sqltypes.Float: RedisFloat, sqltypes.Numeric: RedisNumeric}
    insert_returning = True
    update_returning = True
    delete_returning = True
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

    def get_table_options(self, connection, table_name, schema=None, **kw):
        if not self.has_table(connection, table_name, schema=schema):
            raise exc.NoSuchTableError(table_name)
        # Redis has no PostgreSQL access methods, tablespaces or inheritance.
        return {}

    def get_multi_columns(self, connection, schema=None, filter_names=None, **kw):
        for key, columns in super().get_multi_columns(connection, schema=schema, filter_names=filter_names, **kw):
            # PostgreSQL derives this from sequence/identity defaults. Neither
            # exists here; leave SQLAlchemy's generic reflection policy intact.
            yield key, [{name: value for name, value in column.items() if name != "autoincrement"}
                        for column in columns]

    def get_multi_pk_constraint(self, connection, schema=None, filter_names=None, **kw):
        for key, constraint in super().get_multi_pk_constraint(connection, schema=schema, filter_names=filter_names, **kw):
            yield key, {name: value for name, value in constraint.items() if name != "comment"}

    if hasattr(default.DefaultDialect, "get_multi_table_options"):
        get_multi_table_options = default.DefaultDialect.get_multi_table_options
        get_multi_indexes = default.DefaultDialect.get_multi_indexes

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
