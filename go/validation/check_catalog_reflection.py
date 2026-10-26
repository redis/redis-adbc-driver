"""Run stock SQLAlchemy PostgreSQL reflection over the real ADBC driver.

Usage: REDIS_ADBC_LIBRARY=/absolute/path/to/library python check_catalog_reflection.py
Requires sqlalchemy, adbc-driver-manager and pyarrow. No reflection SQL is
rewritten: the facade only compiles SQLAlchemy expressions and adapts result rows.
"""

import os

import adbc_driver_manager.dbapi as dbapi
from sqlalchemy.dialects.postgresql.base import PGDialect
from sqlalchemy.engine.result import IteratorResult, SimpleResultMetaData


class Connection:
    def __init__(self, raw, dialect):
        self.raw = raw
        self.dialect = dialect

    def execute(self, query, params=None):
        if params:
            query = query.params(**params)
        compiled = query.compile(dialect=self.dialect)
        expanded = compiled.construct_expanded_state()
        sql = expanded.statement
        values = tuple(expanded.parameters[name] for name in expanded.positiontup)
        try:
            with self.raw.cursor() as cur:
                cur.execute(sql, values or None)
                names = [d[0] for d in cur.description]
                rows = cur.fetchall()
            processors = [
                entry.type.dialect_impl(self.dialect).result_processor(self.dialect, None)
                for entry in compiled._result_columns
            ]
            return IteratorResult(SimpleResultMetaData(names, _processors=processors), iter(rows))
        except Exception:
            print("Failing reflection query:\n", sql)
            raise

    def scalars(self, query, params=None):
        return self.execute(query, params).scalars()

    def scalar(self, query, params=None):
        return self.execute(query, params).scalar()


def main():
    with dbapi.connect(
        driver=os.environ["REDIS_ADBC_LIBRARY"],
        entrypoint="AdbcDriverRedisInit",
        db_kwargs={"uri": os.environ.get("REDIS_URI", "redis://localhost:6379/0")},
        autocommit=True,
    ) as raw:
        def execute(sql):
            with raw.cursor() as cur:
                cur.execute(sql)

        execute("DROP SCHEMA IF EXISTS it_sa_catalog CASCADE")
        execute("CREATE SCHEMA it_sa_catalog")
        try:
            execute('CREATE TABLE it_sa_catalog.example (id integer, name varchar(20) COLLATE "C", note text DEFAULT \'hello\', PRIMARY KEY(id,name), CHECK (id>0))')
            execute("COMMENT ON TABLE it_sa_catalog.example IS 'catalog table'")
            execute("COMMENT ON COLUMN it_sa_catalog.example.note IS 'column comment'")
            execute("CREATE VIEW it_sa_catalog.example_view AS SELECT id,name FROM it_sa_catalog.example")
            dialect = PGDialect(paramstyle="qmark")
            dialect.server_version_info = (16, 0)
            dialect.default_schema_name = "public"
            conn = Connection(raw, dialect)
            assert "it_sa_catalog" in dialect.get_schema_names(conn)
            assert dialect.get_table_names(conn, schema="it_sa_catalog") == ["example"]
            assert dialect.get_view_names(conn, schema="it_sa_catalog") == ["example_view"]
            cols = dialect.get_columns(conn, "example", schema="it_sa_catalog")
            assert [c["name"] for c in cols] == ["id", "name", "note"], cols
            assert cols[1]["type"].length == 20 and cols[1]["type"].collation == "C", cols
            assert str(cols[2]["type"]) == "TEXT", cols
            assert cols[2]["comment"] == "column comment", cols
            assert cols[2]["default"] == "'hello'", cols
            key = dialect.get_pk_constraint(conn, "example", schema="it_sa_catalog")
            assert key["constrained_columns"] == ["id", "name"], key
            indexes = dialect.get_indexes(conn, "example", schema="it_sa_catalog")
            assert len(indexes) == 1 and indexes[0]["column_names"] == ["id", "name", "note"], indexes
            assert dialect.get_table_comment(conn, "example", schema="it_sa_catalog")["text"] == "catalog table"
            checks = dialect.get_check_constraints(conn, "example", schema="it_sa_catalog")
            assert len(checks) == 1, checks
            print("PASS: stock PostgreSQL reflection: schemas, tables, views, columns, defaults, comments, primary keys, indexes, checks")
        finally:
            execute("DROP SCHEMA it_sa_catalog CASCADE")


if __name__ == "__main__":
    main()
