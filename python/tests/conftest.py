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
