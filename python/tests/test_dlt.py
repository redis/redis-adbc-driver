"""Actual dlt extract/normalize/load pipelines through the native ADBC driver."""

from datetime import date, datetime, timezone
from decimal import Decimal
import json
from uuid import uuid4

import dlt
import pyarrow as pa
import pytest
from adbc_driver_redis import dbapi
from adbc_driver_redis.dlt import _quote, redis_adbc
from dlt.common.destination.exceptions import DestinationTerminalException
from dlt.pipeline.exceptions import PipelineStepFailed


@pytest.fixture
def pipeline_factory(tmp_path, monkeypatch):
    monkeypatch.setenv("RUNTIME__DLTHUB_TELEMETRY", "false")

    def make(**kwargs):
        return dlt.pipeline(
            pipeline_name="redis_test_" + uuid4().hex,
            pipelines_dir=str(tmp_path),
            **kwargs,
        )

    return make


@pytest.fixture
def target(uri):
    # Exercise quoted schema names, with isolated storage for every test.
    schema = 'it_dlt_"' + uuid4().hex
    yield uri, schema
    with dbapi.connect(uri) as conn, conn.cursor() as cur:
        cur.execute(f"DROP SCHEMA IF EXISTS {_quote(schema)} CASCADE")


def read(target, table="events"):
    uri, schema = target
    with dbapi.connect(uri) as conn, conn.cursor() as cur:
        cur.execute(f"SELECT * FROM {_quote(schema)}.{_quote(table)} ORDER BY id")
        return cur.fetch_arrow_table()


def test_pipeline_batches_and_append(target, pipeline_factory, monkeypatch):
    uri, schema = target
    pipeline = pipeline_factory(
        destination=redis_adbc(uri=uri, schema_name=schema, batch_size=2),
        dataset_name="does_not_select_the_redis_schema",
    )
    # Observe the actual ADBC calls while still executing the native ingestion.
    from adbc_driver_manager.dbapi import Cursor

    counts = []
    ingest = Cursor.adbc_ingest

    def record_ingest(self, table_name, data, **kwargs):
        counts.append(data.num_rows)
        return ingest(self, table_name, data, **kwargs)

    monkeypatch.setattr(Cursor, "adbc_ingest", record_ingest)
    rows = [{"id": n, "label": f"row {n}"} for n in range(5)]
    info = pipeline.run(rows, table_name="events")
    assert not info.has_failed_jobs
    assert counts == [2, 2, 1]
    assert read(target).to_pylist() == rows
    assert not any(c.startswith("_dlt") for c in read(target).column_names)
    assert not pipeline.run(
        [{"id": 5, "label": "later"}], table_name="events"
    ).has_failed_jobs
    assert read(target).to_pylist() == rows + [{"id": 5, "label": "later"}]
    with dbapi.connect(uri) as conn, conn.cursor() as cur:
        cur.execute(
            "SELECT table_name FROM information_schema.tables WHERE table_schema = ?",
            (schema,),
        )
        assert cur.fetchall() == [("events",)]


def test_pipeline_types_and_nulls(target, pipeline_factory):
    uri, schema = target
    pipeline = pipeline_factory(
        destination=redis_adbc(
            uri=uri,
            schema_name=schema,
            db_kwargs={"adbc.redis.time_zone": "UTC"},
            index_columns="id",
        )
    )
    rows = [
        {
            "id": 1,
            "label": "café 'quoted'",
            "active": True,
            "amount": Decimal("1234.5678"),
            "day": date(2026, 10, 29),
            "at": datetime(2026, 10, 29, 12, 34, 56, 123456, timezone.utc),
            "payload": b"\x00\x01\xff",
            "ratio": 1.25,
        },
        {
            "id": 2,
            **{
                c: None
                for c in ("label", "active", "amount", "day", "at", "payload", "ratio")
            },
        },
    ]
    assert not pipeline.run(rows, table_name="events").has_failed_jobs
    assert read(target).to_pylist() == rows


def test_arrow_input_and_multiple_tables(target, pipeline_factory):
    uri, schema = target
    pipeline = pipeline_factory(destination=redis_adbc(uri=uri, schema_name=schema))
    events = dlt.resource(pa.table({"id": [1, 2]}), name="events")
    other = dlt.resource([{"id": 3, "description": "other table"}], name="other")
    assert not pipeline.run([events, other]).has_failed_jobs
    assert read(target).to_pylist() == [{"id": 1}, {"id": 2}]
    assert read(target, "other").to_pylist() == [
        {"id": 3, "description": "other table"}
    ]


def test_incremental_source_keeps_local_checkpoint(target, pipeline_factory):
    uri, schema = target
    pipeline = pipeline_factory(destination=redis_adbc(uri=uri, schema_name=schema))
    rows = [{"id": 1}, {"id": 2}]

    @dlt.resource(name="events", primary_key="id")
    def events(cursor=dlt.sources.incremental("id")):
        yield rows

    assert not pipeline.run(events()).has_failed_jobs
    rows.append({"id": 3})
    # Recreate the pipeline from its local directory, as a subsequent process would.
    pipeline = dlt.pipeline(
        pipeline_name=pipeline.pipeline_name,
        pipelines_dir=pipeline.pipelines_dir,
        destination=redis_adbc(uri=uri, schema_name=schema),
    )
    assert not pipeline.run(events()).has_failed_jobs
    assert read(target).to_pylist() == rows


@pytest.mark.parametrize("disposition", ["replace", "merge"])
def test_unsupported_disposition_fails_before_connect(
    disposition, pipeline_factory, monkeypatch
):
    def forbidden(*args, **kwargs):
        pytest.fail("unsupported dispositions must not connect or modify Redis")

    monkeypatch.setattr(dbapi, "connect", forbidden)
    pipeline = pipeline_factory(destination=redis_adbc(uri="redis://unused:6379/0"))
    with pytest.raises(PipelineStepFailed, match="append.*only"):
        pipeline.run(
            [{"id": 1}],
            table_name="events",
            primary_key="id",
            write_disposition=disposition,
        )


@pytest.mark.parametrize("name", ["", "bad\x00name", None])
def test_invalid_identifiers(name):
    with pytest.raises(DestinationTerminalException, match="SQL names"):
        _quote(name)


def test_secret_configuration(target, pipeline_factory, monkeypatch):
    uri, schema = target
    monkeypatch.setenv("DESTINATION__REDIS_ADBC__URI", uri)
    pipeline = pipeline_factory(destination=redis_adbc(schema_name=schema))
    assert not pipeline.run([{"id": 42}], table_name="events").has_failed_jobs
    assert read(target).to_pylist() == [{"id": 42}]


def test_retry_resumes_after_checkpointed_batch(target, pipeline_factory, monkeypatch):
    from adbc_driver_manager.dbapi import Cursor
    from dlt.common.destination.exceptions import DestinationTransientException

    uri, schema = target
    monkeypatch.setenv("LOAD__RAISE_ON_MAX_RETRIES", "1")
    pipeline = pipeline_factory(
        destination=redis_adbc(uri=uri, schema_name=schema, batch_size=2)
    )
    ingest = Cursor.adbc_ingest

    def fail_second_batch(self, table_name, data, **kwargs):
        if data.column("id")[0].as_py() == 2:
            raise DestinationTransientException("injected failure before writing batch")
        return ingest(self, table_name, data, **kwargs)

    monkeypatch.setattr(Cursor, "adbc_ingest", fail_second_batch)
    rows = [{"id": n} for n in range(5)]
    with pytest.raises(PipelineStepFailed):
        pipeline.run(rows, table_name="events")
    assert read(target).to_pylist() == rows[:2]
    monkeypatch.setattr(Cursor, "adbc_ingest", ingest)
    assert not pipeline.run().has_failed_jobs
    assert read(target).to_pylist() == rows


def test_nested_values_as_json(target, pipeline_factory):
    uri, schema = target
    pipeline = pipeline_factory(destination=redis_adbc(uri=uri, schema_name=schema))
    payload = {"name": "nested", "tags": ["a", "b"]}
    assert not pipeline.run(
        [{"id": 1, "payload": payload}], table_name="events"
    ).has_failed_jobs
    result = read(target).to_pylist()
    assert result[0]["id"] == 1
    assert json.loads(result[0]["payload"]) == payload
