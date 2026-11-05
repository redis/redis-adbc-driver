"""Read/write evidence is required even when a guide process succeeds."""

import importlib.util
from pathlib import Path
from subprocess import CompletedProcess
import sys

import pytest

spec = importlib.util.spec_from_file_location(
    "guide_check", Path(__file__).resolve().parents[1] / "scripts/check_integration_guide.py"
)
checker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checker)


@pytest.mark.parametrize("code,output", [(0, "read-only success"), (1, "REDIS_ADBC_READ_WRITE_OK")])
def test_reject_missing_evidence_or_failed_process(monkeypatch, code, output):
    monkeypatch.setattr(sys, "argv", ["check_integration_guide.py", "numpy", "--require-read-write"])
    monkeypatch.setattr(checker.subprocess, "run", lambda *a, **k: CompletedProcess(a, code, output))
    with pytest.raises(SystemExit if code else RuntimeError):
        checker.main()


def test_accept_verified_output(monkeypatch, capsys):
    monkeypatch.setattr(sys, "argv", ["check_integration_guide.py", "numpy", "--require-read-write"])
    monkeypatch.setattr(checker.subprocess, "run", lambda *a, **k: CompletedProcess(a, 0, "REDIS_ADBC_READ_WRITE_OK rows=3\n"))
    checker.main()
    assert '"write": "passed"' in capsys.readouterr().out
