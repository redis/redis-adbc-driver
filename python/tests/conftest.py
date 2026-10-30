import os

import pytest
from adbc_driver_redis import dbapi


@pytest.fixture
def uri():
    value = os.environ.get("REDIS_URI")
    if not value:
        pytest.skip("REDIS_URI must name a disposable Redis instance with Search")
    return value


@pytest.fixture
def connection(uri):
    with dbapi.connect(uri) as conn:
        yield conn


@pytest.fixture
def workflow_rows(connection):
    from uuid import uuid4

    import pyarrow as pa

    name = "it_workflow_" + uuid4().hex
    expected = pa.table(
        {
            "id": [1, 2, 3],
            "label": ["alpha", None, "雪"],
            "amount": [2.5, None, 7.5],
            "active": [True, None, False],
        }
    )
    try:
        with connection.cursor() as cur:
            cur.adbc_ingest(name, expected, mode="create")
        yield name, expected
    finally:
        with connection.cursor() as cur:
            cur.execute(f'DROP TABLE IF EXISTS "{name}"')
