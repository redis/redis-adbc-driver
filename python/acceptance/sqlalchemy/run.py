"""Keep authenticated connection URLs out of process arguments and config files."""

import os
from pathlib import Path
import sys
from uuid import uuid4

import pytest
from adbc_driver_redis import dbapi

root = Path(__file__).resolve().parent
os.chdir(root)
native_uri = os.environ["REDIS_URI"]
uri = native_uri.replace("://", "+adbc://", 1)
schema = "it_acceptance_sa_" + uuid4().hex
os.environ["REDIS_ACCEPTANCE_SCHEMA"] = schema
with dbapi.connect(native_uri) as setup, setup.cursor() as cursor:
    cursor.execute(f'CREATE SCHEMA "{schema}"')
    try:
        arguments = [
            "--rootdir", str(root), "--dburi", uri,
            "-q", "-ra", "--tb=short", "--junitxml", sys.argv[1], str(root / "test_suite.py"),
        ]
        if os.environ.get("REDIS_ACCEPTANCE_PYTEST_FILTER"):
            arguments.extend(["-k", os.environ["REDIS_ACCEPTANCE_PYTEST_FILTER"]])
        code = pytest.main(arguments)
    finally:
        cursor.execute(f'DROP SCHEMA "{schema}" CASCADE')
raise SystemExit(code)
