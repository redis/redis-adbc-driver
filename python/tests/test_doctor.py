"""Diagnostic failures must be actionable without exposing connection secrets."""

import io
import sys
from types import SimpleNamespace

import pytest

from adbc_driver_redis import doctor


@pytest.fixture
def installed(monkeypatch):
    monkeypatch.setattr(doctor, "_driver_path", lambda: "/test/driver.so")
    monkeypatch.setattr(doctor.ctypes, "CDLL", lambda path: SimpleNamespace(AdbcDriverInit=object()))


def test_offline_does_not_connect(installed, monkeypatch):
    monkeypatch.setattr(doctor, "_query", lambda *args: pytest.fail("offline connected"))
    output = io.StringIO()
    assert doctor.diagnose(output=output) == 0
    assert "SKIP: Connection" in output.getvalue()


def test_missing_library(monkeypatch):
    def missing():
        raise FileNotFoundError("sensitive path")

    monkeypatch.setattr(doctor, "_driver_path", missing)
    output = io.StringIO()
    assert doctor.diagnose(output=output) == 1
    assert "--force-reinstall" in output.getvalue()
    assert "sensitive" not in output.getvalue()


@pytest.mark.parametrize("error,hint", [
    ("WRONGPASS", "Authentication failed"),
    ("NOPERM FT._LIST", "denied a command"),
    ("x509 certificate", "TLS failed"),
    ("search (RediSearch) module", "Search is unavailable"),
    ("Redis Flex", "Flex is unsupported"),
    ("i/o timeout", "timed out"),
    ("unknown failure", "Connection/query failed"),
])
def test_errors_are_redacted(installed, monkeypatch, error, hint):
    secret = "rediss://user:secret@host:1234/0?client_name=private"

    def fail(*args):
        raise RuntimeError(f"{error}: {secret}")

    monkeypatch.setattr(doctor, "_query", fail)
    output = io.StringIO()
    assert doctor.diagnose(uri=secret, output=output) == 1
    assert hint in output.getvalue()
    assert "secret" not in output.getvalue()
    assert "private" not in output.getvalue()


def test_manifest_checks(installed, monkeypatch):
    calls = []
    monkeypatch.setattr(doctor, "_query", lambda *args: calls.append(args))
    assert doctor.diagnose(uri="redis://host", check_manifests=True, output=io.StringIO()) == 0
    assert calls == [("redis://host",), ("redis://host", "redis"), ("redis://host", "rediss")]


def test_manifest_failure(installed, monkeypatch):
    def query(uri, driver=None):
        if driver:
            raise RuntimeError("private manifest failure")

    monkeypatch.setattr(doctor, "_query", query)
    output = io.StringIO()
    assert doctor.diagnose(uri="redis://host", check_manifests=True, output=output) == 1
    assert "install-manifest --user" in output.getvalue()
    assert "private" not in output.getvalue()


@pytest.mark.parametrize("rows,success", [([(1,)], True), ([], False), ([(2,)], False)])
def test_query_fetches_and_closes(monkeypatch, rows, success):
    from contextlib import contextmanager
    from adbc_driver_redis import dbapi

    events = []

    class Cursor:
        def execute(self, sql):
            events.append(sql)

        def fetchall(self):
            events.append("fetch")
            return rows

    @contextmanager
    def cursor():
        try:
            yield Cursor()
        finally:
            events.append("cursor closed")

    @contextmanager
    def connect(uri, **kwargs):
        assert kwargs["db_kwargs"]["adbc.redis.read_timeout"] == "5s"
        try:
            yield SimpleNamespace(cursor=cursor)
        finally:
            events.append("connection closed")

    monkeypatch.setattr(dbapi, "connect", connect)
    if success:
        doctor._query("redis://host")
    else:
        with pytest.raises(ValueError, match="Unexpected"):
            doctor._query("redis://host")
    assert events == ["SELECT 1 AS one", "fetch", "cursor closed", "connection closed"]


def test_cli_offline_ignores_environment(installed, monkeypatch, capsys):
    from adbc_driver_redis.__main__ import main

    monkeypatch.setenv("REDIS_URI", "redis://secret")
    monkeypatch.setattr(sys, "argv", ["redis", "doctor", "--offline"])
    monkeypatch.setattr(doctor, "_query", lambda *args: pytest.fail("offline connected"))
    assert main() == 0
    assert "secret" not in capsys.readouterr().out
    monkeypatch.setattr(sys, "argv", ["redis", "doctor", "--offline", "--check-manifests"])
    with pytest.raises(SystemExit) as error:
        main()
    assert error.value.code == 2


def test_live_driver_and_discovery(uri, tmp_path, monkeypatch):
    from adbc_driver_redis import _driver_path
    from adbc_driver_redis.manifest import install_manifest

    install_manifest(_driver_path(), directory=tmp_path)
    monkeypatch.setenv("ADBC_DRIVER_PATH", str(tmp_path))
    assert doctor.diagnose(uri=uri, check_manifests=True) == 0


def test_ca_bundle_reaches_all_discovery_queries(installed, monkeypatch):
    calls = []
    monkeypatch.setattr(doctor, "_query", lambda *args, **kwargs: calls.append((args, kwargs)))
    assert doctor.diagnose(uri="rediss://host", ca_file="test-ca.pem", check_manifests=True, output=io.StringIO()) == 0
    assert len(calls) == 3
    assert all(kwargs == {"ca_file": "test-ca.pem"} for _, kwargs in calls)


def test_cli_ca_bundle_requires_online_uri(monkeypatch):
    from adbc_driver_redis.__main__ import main

    monkeypatch.delenv("REDIS_URI", raising=False)
    monkeypatch.setattr(sys, "argv", ["redis", "doctor", "--ca-file", "test.pem"])
    with pytest.raises(SystemExit) as error:
        main()
    assert error.value.code == 2
