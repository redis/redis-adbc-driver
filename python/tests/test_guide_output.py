"""Validate example output ownership, row order and cleanup against Redis."""

import importlib.util
from pathlib import Path

import pyarrow as pa
import pytest

spec = importlib.util.spec_from_file_location(
    "guide_data", Path(__file__).resolve().parents[1] / "examples/integration_guide_data.py"
)
guide = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guide)


@pytest.mark.parametrize("empty", [False, True])
def test_output_roundtrip(uri, monkeypatch, empty):
    monkeypatch.setenv("REDIS_URI", uri)
    # Column quoting, duplicate rows, row order, NULLs and Unicode survive.
    table = pa.table({'a"b': [3, 1, 1], "label": ["雪", None, None]})
    if empty:
        table = table.slice(0, 0)
    assert guide.write_result(table).equals(table, check_metadata=False)


def test_output_cleanup_on_verification_failure(uri, monkeypatch):
    monkeypatch.setenv("REDIS_URI", uri)
    # A fresh reader connection failure must still remove the output table.
    connect = guide.dbapi.connect
    calls = []
    from uuid import uuid4
    identifier = uuid4()
    monkeypatch.setattr(guide, "uuid4", lambda: identifier)

    def fail_reader(*args, **kwargs):
        calls.append(None)
        if len(calls) == 2:
            raise RuntimeError("injected reader failure")
        return connect(*args, **kwargs)

    monkeypatch.setattr(guide.dbapi, "connect", fail_reader)
    with pytest.raises(RuntimeError, match="injected reader failure"):
        guide.write_result(pa.table({"id": [1]}))
    with connect(uri) as conn, conn.cursor() as cur:
        cur.execute("SELECT table_name FROM information_schema.tables WHERE table_name=?",
                    ("guide_output_" + identifier.hex,))
        assert cur.fetchall() == []
