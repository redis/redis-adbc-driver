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


def pytest_sessionstart(session):
    from urllib.parse import parse_qs, urlsplit

    for name in ("REDIS_URI", "REDIS_ADMIN_URI", "REDIS_FLEX_URI"):
        value = os.environ.get(name)
        if not value:
            continue
        try:
            uri = urlsplit(value)
            valid = (uri.scheme == "rediss" and uri.hostname and uri.username and uri.password
                     and not {"skip_verify", "ssl_cert_reqs", "ssl_check_hostname"}.intersection(parse_qs(uri.query)))
        except ValueError:
            valid = False
        if not valid:
            raise pytest.UsageError(
                f"{name} must use rediss:// with a username and password on Redis Enterprise; trust its TLS CA"
            )
