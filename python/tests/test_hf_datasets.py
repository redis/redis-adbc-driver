"""Custom Redis ADBC -> Arrow -> Hugging Face Dataset workflow, entirely local."""

import pytest
from adbc_driver_redis import dbapi


@pytest.mark.parametrize("empty", [False, True])
def test_arrow_dataset_save_reload_and_filter(
    uri, workflow_rows, tmp_path, monkeypatch, empty
):
    name, expected = workflow_rows
    monkeypatch.setenv("HF_HUB_OFFLINE", "1")
    monkeypatch.setenv("HF_DATASETS_OFFLINE", "1")
    monkeypatch.setenv("HF_HOME", str(tmp_path / "cache"))
    datasets = pytest.importorskip("datasets")
    predicate = " WHERE FALSE" if empty else ""
    with dbapi.connect(uri) as conn, conn.cursor() as cur:
        cur.execute(f'SELECT * FROM "{name}"{predicate} ORDER BY id')
        table = cur.fetch_arrow_table()
    expected = expected.slice(0, 0) if empty else expected

    dataset = datasets.Dataset(table)
    assert dataset.to_list() == expected.to_pylist()
    assert dataset.data.schema.remove_metadata() == expected.schema.remove_metadata()
    path = tmp_path / "saved_dataset"
    # datasets 5.1.0 defaults to zero shards for an empty table, which its
    # loader cannot reopen. One shard preserves even the empty result schema.
    dataset.save_to_disk(str(path), num_shards=1)
    restored = datasets.load_from_disk(str(path))
    assert restored.to_list() == expected.to_pylist()
    assert restored.features == dataset.features
    filtered = restored.filter(lambda row: row["id"] >= 2, load_from_cache_file=False)
    assert filtered.to_list() == [row for row in expected.to_pylist() if row["id"] >= 2]
