"""Native ADBC data used by local Ibis expressions and MLflow dataset tracking."""

import hashlib
import json

import pytest
from adbc_driver_redis import dbapi


def read_rows(uri, name, empty):
    with dbapi.connect(uri) as conn, conn.cursor() as cur:
        cur.execute(f'SELECT * FROM "{name}" WHERE id >= ? ORDER BY id', (4 if empty else 1,))
        return cur.fetch_arrow_table()


@pytest.mark.parametrize("empty", [False, True])
def test_ibis_arrow_expressions(uri, workflow_rows, empty):
    ibis = pytest.importorskip("ibis")
    name, expected = workflow_rows
    expected = expected.slice(0, 0) if empty else expected
    table = read_rows(uri, name, empty)
    backend = ibis.duckdb.connect()
    try:
        expression = ibis.memtable(table)
        result = backend.to_pyarrow(expression.order_by("id"))
        assert result.equals(expected, check_metadata=False)
        filtered = backend.to_pyarrow(expression.filter(expression.id >= 2).order_by("id"))
        assert filtered.to_pylist() == [r for r in expected.to_pylist() if r["id"] >= 2]
        stats = backend.to_pyarrow(expression.aggregate(
            rows=expression.count(), present=expression.amount.count(), total=expression.amount.sum()
        )).to_pylist()
        assert stats == [{"rows": 0, "present": 0, "total": None} if empty else
                         {"rows": 3, "present": 2, "total": 10.0}]
    finally:
        backend.disconnect()


@pytest.mark.parametrize("empty", [False, True])
def test_mlflow_dataset_tracking(uri, workflow_rows, tmp_path, monkeypatch, empty):
    monkeypatch.setenv("MLFLOW_DISABLE_AGENT_HINT", "1")
    monkeypatch.setenv("MLFLOW_ENABLE_SYSTEM_METRICS_LOGGING", "false")
    mlflow = pytest.importorskip("mlflow")
    pandas = pytest.importorskip("pandas")
    import pyarrow as pa
    from mlflow.tracking import MlflowClient

    name, expected = workflow_rows
    expected = expected.slice(0, 0) if empty else expected
    table = read_rows(uri, name, empty)
    assert table.equals(expected, check_metadata=True)
    monkeypatch.setenv("MLFLOW_TRACKING_URI", "sqlite:///" + str(tmp_path / "tracking.db"))
    client = MlflowClient()
    experiment = client.create_experiment("native_adbc", artifact_location=(tmp_path / "artifacts").as_uri())
    options = {}
    if empty:
        # MLflow 3.17.2's automatic digest fails on a pandas 3.1 empty string
        # column. Its public explicit-digest option avoids that upstream path.
        sink = pa.BufferOutputStream()
        with pa.ipc.new_stream(sink, table.schema) as writer:
            writer.write_table(table)
        options["digest"] = hashlib.sha256(sink.getvalue()).hexdigest()[:32]
    dataset = mlflow.data.from_pandas(table.to_pandas(), name="native_redis_rows", **options)
    pandas.testing.assert_frame_equal(dataset.df, table.to_pandas())
    with mlflow.start_run(experiment_id=experiment) as run:
        mlflow.log_input(dataset, context="testing")
        mlflow.log_metric("rows_checked", table.num_rows)
        mlflow.log_dict({"rows": table.to_pylist(), "schema": str(table.schema)}, "checked-input.json")
    stored = client.get_run(run.info.run_id)
    assert stored.info.status == "FINISHED"
    assert stored.data.metrics["rows_checked"] == expected.num_rows
    assert len(stored.inputs.dataset_inputs) == 1
    logged = stored.inputs.dataset_inputs[0]
    assert logged.dataset.name == dataset.name
    assert logged.dataset.digest == dataset.digest
    assert json.loads(logged.dataset.profile) == {"num_rows": expected.num_rows, "num_elements": expected.num_rows * 4}
    schema = json.loads(logged.dataset.schema)
    assert [column["name"] for column in schema["mlflow_colspec"]] == expected.column_names
    assert [column["type"] for column in schema["mlflow_colspec"]] == ["long", "string", "double", "boolean"]
    assert {tag.key: tag.value for tag in logged.tags}["mlflow.data.context"] == "testing"
    path = client.download_artifacts(run.info.run_id, "checked-input.json", str(tmp_path / "download"))
    with open(path) as file:
        artifact = json.load(file)
    assert artifact == {"rows": expected.to_pylist(), "schema": str(expected.schema)}


class MLflowEmptyDigestCompatibilityError(AssertionError):
    pass


# https://github.com/mlflow/mlflow/issues/27358
@pytest.mark.xfail(raises=MLflowEmptyDigestCompatibilityError, strict=True,
                   reason="MLflow 3.17.2 automatic digest rejects pandas 3.1.0 empty string columns")
def test_mlflow_default_empty_digest(uri, workflow_rows, monkeypatch):
    monkeypatch.setenv("MLFLOW_DISABLE_AGENT_HINT", "1")
    mlflow = pytest.importorskip("mlflow")
    pandas = pytest.importorskip("pandas")
    name, _ = workflow_rows
    frame = read_rows(uri, name, True).to_pandas()
    try:
        dataset = mlflow.data.from_pandas(frame, name="empty_native_rows")
    except NotImplementedError as error:
        if (mlflow.__version__ == "3.17.2" and pandas.__version__ == "3.1.0"
                and str(error) == "eq not implemented for <class 'type'>"):
            raise MLflowEmptyDigestCompatibilityError(str(error)) from error
        raise
    assert dataset.profile["num_rows"] == 0
