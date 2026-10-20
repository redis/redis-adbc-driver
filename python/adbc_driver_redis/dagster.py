"""Dagster asset storage over Arrow; writes are not transactional."""

import uuid

import pyarrow as pa
from dagster import ConfigurableIOManager, TableColumn, TableSchema
from pydantic import Field

from . import dbapi


def _quote(name):
    if not isinstance(name, str) or not name or "\x00" in name:
        raise ValueError("SQL names must be nonempty strings without NUL")
    return '"' + name.replace('"', '""') + '"'


def _arrow(obj):
    if isinstance(obj, (pa.Table, pa.RecordBatchReader)):
        return obj
    if type(obj).__module__.split(".")[0] == "pandas":
        return pa.Table.from_pandas(obj, preserve_index=False)
    if type(obj).__module__.split(".")[0] == "polars":
        return obj.to_arrow()
    raise TypeError(
        "Redis IO manager accepts Arrow tables/readers, pandas or polars DataFrames"
    )


class RedisADBCIOManager(ConfigurableIOManager):
    """Persist asset outputs as tables; serialize writes to the same asset.

    Unpartitioned writes stage before DROP/RENAME (a brief missing-table window).
    Partition writes DELETE then append; retries replace the partition. A failed
    partition append may leave partial rows. No rollback or concurrent-writer
    isolation is promised.
    """

    uri: str
    db_kwargs: dict[str, str] = Field(default_factory=dict)
    schema_name: str | None = None
    index_columns: str = "*"

    def _table(self, context):
        parts = context.asset_key.path
        schema = self.schema_name or (
            "_".join(parts[:-1]) if len(parts) > 1 else "public"
        )
        return schema.lower(), parts[-1].lower()

    def _partition(self, context):
        if not context.has_asset_partitions:
            return "", ()
        column = context.definition_metadata.get("partition_expr")
        if not isinstance(column, str):
            raise TypeError(
                "Partitioned assets require metadata partition_expr naming one column"
            )
        col = _quote(column)
        from dagster import MultiPartitionsDefinition, TimeWindowPartitionsDefinition

        if isinstance(context.asset_partitions_def, MultiPartitionsDefinition):
            raise TypeError("Multi-dimensional partitions are unsupported")
        if isinstance(context.asset_partitions_def, TimeWindowPartitionsDefinition):
            window = context.asset_partitions_time_window
            return f"{col} >= ? AND {col} < ?", (window.start, window.end)
        keys = list(context.asset_partition_keys)
        if not all(isinstance(key, str) for key in keys):
            raise ValueError(
                "Only static/dynamic string or time-window partitions are supported"
            )
        if not keys:
            return "FALSE", ()
        return f"{col} IN ({','.join('?' for _ in keys)})", tuple(keys)

    def handle_output(self, context, obj):
        data = _arrow(obj)
        schema, table = self._table(context)
        target = f"{_quote(schema)}.{_quote(table)}"
        predicate, params = self._partition(context)
        with (
            dbapi.connect(self.uri, db_kwargs=self.db_kwargs) as conn,
            conn.cursor() as cur,
        ):
            cur.execute(f"CREATE SCHEMA IF NOT EXISTS {_quote(schema)}")
            cur.adbc_statement.set_options(
                **{"adbc.redis.ingest.index_columns": self.index_columns}
            )
            if predicate:
                # Missing table is expected on first materialization only.
                cur.execute(
                    "SELECT table_name FROM information_schema.tables WHERE table_schema = ? AND table_name = ?",
                    (schema, table),
                )
                exists = bool(cur.fetchall())
                if exists:
                    cur.execute(
                        f"DELETE FROM {target} WHERE {predicate}", params or None
                    )
                count = cur.adbc_ingest(
                    table, data, mode="create_append", db_schema_name=schema
                )
            else:
                stage = f"{table}__dagster_{uuid.uuid4().hex}"
                staged = f"{_quote(schema)}.{_quote(stage)}"
                try:
                    count = cur.adbc_ingest(
                        stage, data, mode="create", db_schema_name=schema
                    )
                    cur.execute(f"DROP TABLE IF EXISTS {target}")
                    cur.execute(f"ALTER TABLE {staged} RENAME TO {_quote(table)}")
                finally:
                    cur.execute(f"DROP TABLE IF EXISTS {staged}")
        context.add_output_metadata(
            {
                "row_count": count,
                "table": f"{schema}.{table}",
                "dagster/column_schema": TableSchema(
                    columns=[
                        TableColumn(name=f.name, type=str(f.type)) for f in data.schema
                    ]
                ),
            }
        )

    def load_input(self, context):
        schema, table = self._table(context)
        columns = context.definition_metadata.get("columns")
        if columns is not None and (
            not isinstance(columns, (list, tuple)) or not columns
        ):
            raise ValueError("columns metadata must be a nonempty list of column names")
        projection = ", ".join(_quote(c) for c in columns) if columns else "*"
        # Partition expression belongs to the producing asset's metadata.
        partition_context = context
        if (
            context.has_asset_partitions
            and "partition_expr" not in context.definition_metadata
        ):
            from types import SimpleNamespace

            partition_context = SimpleNamespace(
                has_asset_partitions=True,
                definition_metadata=context.upstream_output.definition_metadata,
                asset_partitions_def=context.asset_partitions_def,
                asset_partition_keys=context.asset_partition_keys,
            )
            from dagster import TimeWindowPartitionsDefinition

            if isinstance(context.asset_partitions_def, TimeWindowPartitionsDefinition):
                partition_context.asset_partitions_time_window = (
                    context.asset_partitions_time_window
                )
        predicate, params = self._partition(partition_context)
        sql = f"SELECT {projection} FROM {_quote(schema)}.{_quote(table)}"
        if predicate:
            sql += f" WHERE {predicate}"
        with (
            dbapi.connect(self.uri, db_kwargs=self.db_kwargs) as conn,
            conn.cursor() as cur,
        ):
            cur.execute(sql, params or None)
            data = cur.fetch_arrow_table()
        annotation = context.dagster_type.typing_type
        module = getattr(annotation, "__module__", "").split(".")[0]
        if module == "pandas":
            return data.to_pandas()
        if module == "polars":
            import polars as pl

            return pl.from_arrow(data)
        if annotation is pa.RecordBatchReader:
            return data.to_reader()
        return data
