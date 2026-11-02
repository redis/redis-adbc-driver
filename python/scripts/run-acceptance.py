#!/usr/bin/env python3
"""Run upstream acceptance contracts in separate dependency environments.

Requires a built current-checkout library and a disposable Redis Software TLS
fixture with verified 4x QPF (make -C go redis-up). Does not provision servers.
"""

import argparse
import json
import os
from pathlib import Path
import re
import signal
import subprocess
from urllib.parse import parse_qs, unquote, urlsplit
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[2]
SUITES = ("sqlalchemy", "r-dbi", "fivetran", "adbc")


def connection_env(environ):
    env = dict(environ)
    try:
        uri = urlsplit(env.get("REDIS_URI", ""))
        valid = (uri.scheme == "rediss" and uri.hostname and uri.username and uri.password
                 and not {"skip_verify", "ssl_cert_reqs", "ssl_check_hostname"}
                 .intersection(parse_qs(uri.query)))
    except ValueError:
        valid = False
    if not valid:
        raise ValueError("REDIS_URI must name an authenticated, verified TLS disposable Redis Software database")
    library = Path(env.get("REDIS_ADBC_LIBRARY", ""))
    if not library.is_file():
        raise ValueError("Set REDIS_ADBC_LIBRARY to the current checkout's built shared library")
    env["REDIS_ADBC_LIBRARY"] = str(library.resolve())
    env["REDIS_USERNAME"] = unquote(uri.username)
    env["REDIS_PASSWORD"] = unquote(uri.password)
    env["PYTHONPATH"] = str(ROOT / "python")
    env["REDIS_ADBC_REPO"] = str(ROOT)
    # Keep private Redis trust out of uv's package downloads. Restore it only
    # inside the actual test interpreter, before the native library is loaded.
    env["REDIS_ACCEPTANCE_CA_FILE"] = env.pop("SSL_CERT_FILE", "") or env.get("REDIS_TLS_CA_FILE", "")
    return env


def redact(text, env):
    for key in ("REDIS_URI", "REDIS_PASSWORD"):
        if env.get(key):
            text = text.replace(env[key], "[redacted]")
    return re.sub(r"rediss?://[^\s<>\"']+", "[redis-uri]", text)


def report_counts(path):
    counts = dict(passed=0, failed=0, errors=0, skipped=0, xfailed=0)
    for case in ET.parse(path).iter("testcase"):
        skip = case.find("skipped")
        if case.find("failure") is not None:
            kind = "failed"
        elif case.find("error") is not None:
            kind = "errors"
        elif skip is not None:
            kind = "xfailed" if skip.get("type") == "pytest.xfail" else "skipped"
        else:
            kind = "passed"
        counts[kind] += 1
    return counts


def execute(command, cwd, env, timeout):
    """A timed-out suite must not leave workers racing the next suite."""
    process = subprocess.Popen(command, cwd=cwd, env=env, text=True,
                               stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                               start_new_session=os.name == "posix")
    try:
        stdout, _ = process.communicate(timeout=timeout)
    except (subprocess.TimeoutExpired, KeyboardInterrupt):
        if os.name == "posix":
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
        else:
            process.kill()
        process.communicate()
        raise
    return subprocess.CompletedProcess(command, process.returncode, stdout)


def suite_command(suite, report, env):
    uv = env.get("UV", "uv")

    def python(dependencies, script, *args, version="3.14"):
        command = [uv, "run", "--no-project", "--python", version,
                   "--with-editable", str(ROOT / "python")]
        for dependency in dependencies:
            command += ["--with", dependency]
        return [*command, "python", str(ROOT / "python/acceptance/run-python.py"),
                *script, *map(str, args)]

    pytest = ["-m", "pytest", "-q", "-ra", "--tb=short", f"--junitxml={report}"]
    if suite == "sqlalchemy":
        return python(["SQLAlchemy==2.1.6", "pytest>=9,<10", "pyarrow==26.0.0"],
                      [str(ROOT / "python/acceptance/sqlalchemy/run.py")], report), ROOT
    if suite == "r-dbi":
        if env.get("REDIS_ACCEPTANCE_CA_FILE"):
            env["SSL_CERT_FILE"] = env["REDIS_ACCEPTANCE_CA_FILE"]
        return [env.get("RSCRIPT", "Rscript"), str(ROOT / "python/acceptance/dbi/run.R"),
                str(ROOT / "python/acceptance/dbi/test_dbi.R"), str(report)], ROOT
    if suite == "fivetran":
        return python(["fivetran-connector-sdk>=2,<3", "duckdb==2.0.0", "pytest>=9,<10", "pyarrow==26.0.0"],
                      pytest, str(ROOT / "python/tests/test_orchestrator_candidates.py")
                      + "::test_fivetran_sdk_local_tester", version="3.12"), ROOT
    return [uv, "run", "--project", str(ROOT / "go/validation"), "python",
            str(ROOT / "python/acceptance/run-python.py"), *pytest,
            str(ROOT / "go/validation/tests")], ROOT


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("suites", nargs="*", choices=[*SUITES, "all"])
    parser.add_argument("--list", action="store_true")
    parser.add_argument("--results", type=Path, default=Path("acceptance-results"))
    parser.add_argument("--timeout", type=int, default=3600, help="seconds per suite process")
    args = parser.parse_args(argv)
    if args.list:
        print("\n".join(SUITES))
        return 0
    selected = args.suites or ["all"]
    if "all" in selected:
        selected = list(SUITES)
    selected = list(dict.fromkeys(selected))
    try:
        env = connection_env(os.environ)
    except ValueError as error:
        parser.error(str(error))
    results = args.results.resolve()
    results.mkdir(parents=True, exist_ok=True)
    summary = {}
    # Discard the old summary before setup, so interrupted runs cannot reuse it.
    (results / "summary.json").unlink(missing_ok=True)
    for suite in selected:
        report = results / f"{suite}.xml"
        report.unlink(missing_ok=True)
        log = results / f"{suite}.log"
        log.write_text("")
        suite_env = env.copy()

        def run(command, cwd=ROOT):
            result = execute(command, cwd, suite_env, args.timeout)
            with log.open("a") as output:
                output.write(redact(result.stdout, suite_env))
            if result.returncode:
                raise subprocess.CalledProcessError(result.returncode, ["acceptance", suite])

        print(f"Running upstream acceptance: {suite}", flush=True)
        code = 0
        try:
            command, cwd = suite_command(suite, report, suite_env)
            run(command, cwd)
        except (OSError, subprocess.SubprocessError) as error:
            code = getattr(error, "returncode", 1)
            with log.open("a") as output:
                output.write(redact(f"\n{type(error).__name__}: {error} (exit {code})\n", suite_env))
        counts = {}
        status = "no_result"
        if report.exists():
            report.write_text(redact(report.read_text(), suite_env))
            try:
                counts = report_counts(report)
                status = "passed" if (code == 0 and counts["passed"] > 0
                                      and not counts["failed"] and not counts["errors"]) else "failed"
            except ET.ParseError:
                status = "invalid_report"
        summary[suite] = {"status": status, "exit_code": code, **counts}
        print(f"{suite}: {json.dumps(summary[suite])}", flush=True)
        (results / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    return int(any(result["status"] != "passed" for result in summary.values()))


if __name__ == "__main__":
    raise SystemExit(main())
