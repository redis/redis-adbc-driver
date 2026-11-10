#!/usr/bin/env python3
"""Exercise Sling's Redis ADBC path and verify the round trip in PostgreSQL.

Requires Docker, Sling 1.6.6, and the repository's Software setup dependencies.
Private connection details and subprocess output stay under ignored go/build/.
"""

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import uuid


ROOT = Path(__file__).resolve().parents[1]
TEMPLATE = ROOT / "scripts/sling/adbc.yaml"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--sling", required=True, type=Path)
    args = parser.parse_args()
    sling = str(args.sling.resolve())
    run_id = uuid.uuid4().hex[:12]
    work = ROOT / "go/build" / ("sling-" + run_id)
    work.mkdir(parents=True, mode=0o700)
    os.chmod(work, 0o700)
    env = os.environ.copy()
    env.update(SLING_HOME_DIR=str(work / "home"), SLING_DISABLE_TELEMETRY="true")
    schema = "sling_" + run_id
    container = "redis-adbc-sling-" + run_id
    checks = []
    started = False
    connected = False

    def run(name, command, *, input=None, check=True, timeout=300):
        result = subprocess.run(command, cwd=ROOT, env=env, input=input,
                                text=True, capture_output=True, timeout=timeout)
        log = work / (name + ".log")
        log.write_text(result.stdout + result.stderr)
        log.chmod(0o600)
        if check and result.returncode:
            raise RuntimeError(f"{name} failed; inspect private log {log}")
        return result

    def sl(name, *arguments, **kwargs):
        return run(name, [sling, *arguments], **kwargs)

    def pg(name, sql):
        return run(name, ["docker", "exec", "-i", container, "psql", "-U", "postgres",
                          "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1"], input=sql)

    def transfer(name, source, table, target, output, *extra):
        sl(name, "run", "--src-conn", source, "--src-stream", table,
           "--tgt-conn", target, "--tgt-object", output, *extra)
        checks.append(name)

    def compare(name):
        result = pg(name, f"""
          SELECT (SELECT count(*) FROM {schema}.source),
                 (SELECT count(*) FROM {schema}.roundtrip),
                 (SELECT count(*) FROM (
                   (SELECT * FROM {schema}.source EXCEPT ALL SELECT * FROM {schema}.roundtrip)
                   UNION ALL
                   (SELECT * FROM {schema}.roundtrip EXCEPT ALL SELECT * FROM {schema}.source)
                 ) differences);
        """)
        if result.stdout.strip() != "10003|10003|0":
            raise RuntimeError("round-trip row/value mismatch")
        checks.append(name)

    try:
        run("software-setup", ["make", "-C", "go", "redis-up"], timeout=600)
        run("build", ["make", "-C", "go", "build"], timeout=600)
        # The setup above verifies active QPF=4, six Search workers, mnp/32,
        # and noeviction before declaring retained control persistence.
        values = run("read-environment", ["bash", "-c",
            'source go/build/test.env; python3 -c \'import json,os; '
            'print(json.dumps({k:os.environ[k] for k in ["REDIS_URI","SSL_CERT_FILE"]}))\''])
        redis_env = json.loads(values.stdout)
        env.update(redis_env)
        library = ROOT / "go/build" / ("libadbc_driver_redis.dylib" if sys.platform == "darwin"
                                      else "libadbc_driver_redis.so")
        env["REDIS_ADBC"] = json.dumps({
            "type": "adbc", "driver_name": "redis", "driver": str(library),
            "uri": redis_env["REDIS_URI"],
            "adbc.redis.tls_ca_file": redis_env["SSL_CERT_FILE"],
            "adbc.redis.partition_control_persistence": "guaranteed",
            "insert_concurrency": 1,
        })
        version = sl("sling-version", "--version").stdout.strip()
        sl("redis-connect", "conns", "test", "REDIS_ADBC")
        connected = True
        run("postgres-start", ["docker", "run", "--detach", "--name", container,
            "--publish", "127.0.0.1::5432", "--env", "POSTGRES_HOST_AUTH_METHOD=trust",
            "postgres:18"])
        started = True
        port = run("postgres-port", ["docker", "port", container, "5432/tcp"]).stdout.strip().split(":")[-1]
        env["PG_SLING"] = f"postgresql://postgres@127.0.0.1:{port}/postgres?sslmode=disable"
        run("postgres-ready", ["docker", "exec", container, "sh", "-c",
            'for i in $(seq 1 60); do pg_isready -U postgres >/dev/null && exit 0; sleep 1; done; exit 1'])
        pg_version = pg("postgres-version", "SHOW server_version;").stdout.strip()
        pg("seed", f"""
          CREATE SCHEMA {schema};
          CREATE TABLE {schema}.source AS
          SELECT i::bigint AS id,
            CASE WHEN i%11=0 THEN NULL ELSE 'hello 世界, "quoted" '||i||chr(10)||'next' END::text AS label,
            (i%2=0)::boolean AS active, (i::numeric/100)::numeric(18,2) AS amount,
            ('2026-01-01'::date+i%365)::date AS day,
            ('2026-01-01 12:00:00'::timestamp+i*interval '1 microsecond')::timestamp(6) AS happened_at,
            (i*0.125)::double precision AS ratio
          FROM generate_series(1,10000) i;
          INSERT INTO {schema}.source VALUES
            (-9223372036854775808, '', false, -9999999999999999.99, '1969-12-31', '1969-12-31 23:59:59.123456', -0.125),
            (9223372036854775807, NULL, NULL, NULL, NULL, NULL, NULL),
            (9223372036854775807, NULL, NULL, NULL, NULL, NULL, NULL);
        """)
        # Preserve the stock connector failure as evidence, without weakening it
        # into an accepted transfer. A future Sling fix can make this pass.
        stock = sl("stock-full-refresh", "run", "--src-conn", "PG_SLING",
            "--src-stream", f"{schema}.source", "--tgt-conn", "REDIS_ADBC",
            "--tgt-object", f"{schema}.stock", check=False)
        stock_status = "passed" if stock.returncode == 0 else "failed"
        home_templates = work / "home/templates"
        home_templates.mkdir(parents=True, exist_ok=True)
        (home_templates / "adbc.yaml").write_text(TEMPLATE.read_text())
        sl("redis-create", "conns", "exec", "REDIS_ADBC",
            f"CREATE SCHEMA IF NOT EXISTS {schema}",
            f"CREATE TABLE {schema}.inbound (id BIGINT, label TEXT, active BOOLEAN, "
            "amount NUMERIC(18,2), day DATE, happened_at TIMESTAMP(6), ratio DOUBLE PRECISION)")
        empty = sl("empty-arrow", "conns", "exec", "REDIS_ADBC",
            f"SELECT label FROM {schema}.inbound WHERE 1=0", check=False)
        for iteration in range(2):
            transfer(f"pg-to-redis-{iteration}", "PG_SLING", f"{schema}.source",
                     "REDIS_ADBC", f"{schema}.inbound", "--mode", "truncate",
                     "--tgt-options", '{"direct_insert":true}')
            transfer(f"redis-to-pg-{iteration}", "REDIS_ADBC", f"{schema}.inbound",
                     "PG_SLING", f"{schema}.roundtrip", "--mode", "full-refresh")
            compare(f"exact-comparison-{iteration}")
        results = {"sling": version, "postgresql": pg_version, "rows": 10003,
                   "columns": 7, "repetitions": 2, "differing_rows": 0,
                   "stock_full_refresh": stock_status,
                   "empty_arrow_query": "passed" if empty.returncode == 0 else "failed",
                   "checks": checks}
        (work / "results.json").write_text(json.dumps(results, indent=2) + "\n")
        print(json.dumps(results, indent=2))
        print(f"Private logs: {work}")
    finally:
        cleanup_errors = []
        if connected:
            try:
                sl("redis-cleanup", "conns", "exec", "REDIS_ADBC",
                   f"DROP SCHEMA IF EXISTS {schema} CASCADE")
            except Exception as exc:
                cleanup_errors.append(str(exc))
        if started:
            try:
                run("postgres-cleanup", ["docker", "rm", "-f", "-v", container])
            except Exception as exc:
                cleanup_errors.append(str(exc))
        if cleanup_errors:
            raise RuntimeError("; ".join(cleanup_errors))


if __name__ == "__main__":
    main()
