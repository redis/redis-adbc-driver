"""Run native ADBC Python tasks and downstream output checks on local Kestra."""

import json
import os
from pathlib import Path
import shutil
import secrets
import socket
import subprocess
import sys
import textwrap
import time
from uuid import uuid4

from contextlib import contextmanager


@contextmanager
def kestra_server(root):
    __import__("requests")
    import requests
    __import__("yaml")
    import yaml
    java = os.environ.get("KESTRA_JAVA") or shutil.which("java")
    jar = os.environ.get("KESTRA_JAR")
    plugins = os.environ.get("KESTRA_PLUGINS_PATH")
    if not java or not jar or not plugins:
        raise RuntimeError("Kestra requires KESTRA_JAR, KESTRA_PLUGINS_PATH, and Java 25 (KESTRA_JAVA)")
    client = requests.Session()
    client.auth = ("candidate@localhost.test", "T3st" + secrets.token_hex(16))
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    config = root / "config.yml"
    config.write_text(yaml.safe_dump({
        "micronaut": {"server": {"host": "127.0.0.1"}},
        "kestra": {
            "anonymous-usage-report": {"enabled": False},
            "server": {"basic-auth": {"username": client.auth[0], "password": client.auth[1]}},
            "plugins": {"auto-install": {"enabled": False}},
            "tasks": {"tmp-dir": {"path": str(root / "tasks")}},
        },
    }))
    base = f"http://127.0.0.1:{port}"
    log = root / "server.log"
    uv = shutil.which(os.environ.get("UV", "uv"))
    search_path = [str(Path(sys.executable).parent)]
    if uv:
        search_path.append(str(Path(uv).parent))
    server_env = {**os.environ, "PATH": os.pathsep.join([*search_path, os.environ["PATH"]]),
                  "VIRTUAL_ENV": str(Path(sys.executable).parent.parent)}
    with log.open("w") as output:
        process = subprocess.Popen(
            [java, "-Xmx512m", "-XX:ActiveProcessorCount=2", "-jar", str(Path(jar).resolve()),
             "server", "local", "--no-tutorials", "--worker-thread=2", f"--port={port}",
             "-c", str(config), "-p", str(Path(plugins).resolve()), "-l", "WARN"],
            cwd=root, env=server_env, stdout=output, stderr=subprocess.STDOUT,
        )
        try:
            deadline = time.monotonic() + 120
            while time.monotonic() < deadline:
                assert process.poll() is None, log.read_text()
                try:
                    if requests.get(base + "/ping", timeout=2).status_code == 200:
                        break
                except requests.RequestException:
                    pass
                time.sleep(0.5)
            else:
                raise RuntimeError("Kestra startup timed out:\n" + log.read_text())
            yield base, log, client
        finally:
            client.close()
            process.terminate()
            try:
                process.wait(timeout=30)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=10)


def run_tasks(uri, workflow_rows, server, empty=False):
    import yaml

    name, expected = workflow_rows
    expected = expected.slice(0, 0) if empty else expected
    base, log, client = server
    namespace = "redis.adbc.candidates"
    flow_id = "native_" + uuid4().hex
    task_env = {"REDIS_URI": uri, "REDIS_ADBC_TEST_TABLE": name,
                "REDIS_ADBC_LIBRARY": os.environ["REDIS_ADBC_LIBRARY"],
                "PATH": str(Path(sys.executable).parent) + os.pathsep + os.environ["PATH"],
                "PYTHONPATH": os.pathsep.join(sys.path)}
    read = textwrap.dedent('''\
        import json
        import os
        from adbc_driver_redis import dbapi
        from kestra import Kestra
        with dbapi.connect(os.environ["REDIS_URI"]) as conn, conn.cursor() as cur:
            name = os.environ["REDIS_ADBC_TEST_TABLE"]
            cur.execute(f'SELECT * FROM "{name}" WHERE id >= ? ORDER BY id', (MINIMUM,))
            table = cur.fetch_arrow_table()
        with open("read.json", "w") as file:
            json.dump({"rows": table.to_pylist(), "schema": str(table.schema), "pid": os.getpid()}, file)
        Kestra.outputs({"rows_read": table.num_rows})
        ''').replace("MINIMUM", "4" if empty else "1")
    check = textwrap.dedent('''\
        import json
        import os
        from kestra import Kestra
        with open("read.json") as file:
            data = json.load(file)
        assert data["rows"] == EXPECTED_ROWS
        assert data["schema"] == EXPECTED_SCHEMA
        assert data["pid"] != os.getpid()
        Kestra.outputs({"rows_checked": len(data["rows"]), "unicode_checked": True})
        ''').replace("EXPECTED_ROWS", repr(expected.to_pylist())).replace("EXPECTED_SCHEMA", repr(str(expected.schema)))

    def task(identifier, script):
        return {"id": identifier, "type": "io.kestra.plugin.scripts.python.Script",
                "taskRunner": {"type": "io.kestra.plugin.core.runner.Process"},
                # Distinct maps avoid YAML aliases in Kestra Property values.
                "env": dict(task_env), "script": script}

    read_task, check_task = task("read_native", read), task("check_output", check)
    read_task["outputFiles"] = ["read.json"]
    check_task["inputFiles"] = {"read.json": "{{ outputs.read_native.outputFiles['read.json'] }}"}
    flow = {"id": flow_id, "namespace": namespace, "tasks": [read_task, check_task]}
    response = client.post(base + "/api/v1/main/flows", data=yaml.safe_dump(flow),
                             headers={"Content-Type": "application/x-yaml"}, timeout=30)
    assert response.status_code in (200, 201), response.text + "\n" + log.read_text()
    response = client.post(base + f"/api/v1/main/executions/{namespace}/{flow_id}", timeout=30)
    assert response.status_code in (200, 201), response.text
    execution_id = response.json()["id"]
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        response = client.get(base + f"/api/v1/main/executions/{execution_id}", timeout=10)
        response.raise_for_status()
        execution = response.json()
        state = execution["state"]["current"]
        if state in {"SUCCESS", "FAILED", "KILLED", "CANCELLED", "WARNING"}:
            break
        time.sleep(0.5)
    else:
        raise RuntimeError("Kestra execution timed out:\n" + log.read_text())
    if state != "SUCCESS":
        task_logs = client.get(base + f"/api/v1/main/logs/{execution_id}", timeout=10)
        task_logs.raise_for_status()
        errors = [entry["message"] for entry in task_logs.json() if entry["level"] == "ERROR"]
        raise RuntimeError("Kestra task failed:\n" + "\n".join(errors))
    assert [task["state"]["current"] for task in execution["taskRunList"]] == ["SUCCESS", "SUCCESS"]
    task_run_id = execution["taskRunList"][-1]["id"]
    response = client.get(base + f"/api/v1/main/outputs/tasks/{execution_id}/{task_run_id}", timeout=10)
    response.raise_for_status()
    assert response.json()["vars"] == {
        "rows_checked": expected.num_rows, "unicode_checked": True,
    }

if __name__ == '__main__':
    import tempfile
    from integration_guide_data import example_table, read_rows
    with tempfile.TemporaryDirectory(prefix='redis-kestra-guide-') as directory:
        with example_table() as name, kestra_server(Path(directory)) as server:
            run_tasks(os.environ['REDIS_URI'], (name, read_rows(name)), server)
    print('KESTRA_GUIDE_OK')
