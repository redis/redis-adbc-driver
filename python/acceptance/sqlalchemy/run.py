"""Keep authenticated connection URLs out of process arguments and config files."""

import os
from pathlib import Path
import sys

import pytest

root = Path(__file__).resolve().parent
os.chdir(root)
uri = os.environ["REDIS_URI"].replace("://", "+adbc://", 1)
raise SystemExit(pytest.main([
    "--rootdir", str(root), "--dburi", uri,
    "-q", "-ra", "--tb=short", "--junitxml", sys.argv[1], str(root / "test_suite.py"),
]))
