#!/usr/bin/env python3
"""Exercise Sling's Redis ADBC path and verify the round trips in PostgreSQL.

Requires Docker, Sling 1.6.8 or later, and the repository's Software setup
dependencies. The Redis connection uses Sling's stock generic ADBC support,
without a template override or precreated staging target. Every case runs and is
recorded. The exit status is nonzero when any case failed or could not run.
Private connection details and subprocess output stay under ignored go/build/.
"""

import argparse
import csv
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import uuid


ROOT = Path(__file__).resolve().parents[1]
ROWS = 10003
COLUMNS = ("id", "label", "active", "amount", "day", "happened_at", "ratio")
TABLE_DDL = ("(id BIGINT, label TEXT, active BOOLEAN, amount NUMERIC(18,2), day DATE, "
             "happened_at TIMESTAMP(6), ratio DOUBLE PRECISION)")
TYPE_FIELDS = ("column_name, data_type, character_maximum_length, numeric_precision, "
               "numeric_scale, datetime_precision")


class Unavailable(Exception):
    """The Sling build does not provide the command a case needs."""


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
    # The empty-source cases set these themselves; inherited values would hide
    # Sling's default behavior.
    for name in ("SLING_ALLOW_EMPTY", "SLING_ALLOW_EMPTY_TABLES"):
        env.pop(name, None)
    schema = "sling_" + run_id
    container = "redis-adbc-sling-" + run_id
    outcomes = {}
    started = False
    connected = False

    def run(name, command, *, input=None, check=True, timeout=300, extra_env=None):
        result = subprocess.run(command, cwd=ROOT, env={**env, **(extra_env or {})}, input=input,
                                text=True, capture_output=True, timeout=timeout)
        log = work / (name + ".log")
        log.write_text(result.stdout + result.stderr)
        log.chmod(0o600)
        if check and result.returncode:
            raise RuntimeError(f"{name} failed; inspect private log {log}")
        return result

    def sl(name, *arguments, **kwargs):
        return run(name, [sling, *arguments], **kwargs)

    def redis_exec(name, *queries):
        sl(name, "conns", "exec", "REDIS_ADBC", *queries)

    def pg(name, sql):
        return run(name, ["docker", "exec", "-i", container, "psql", "-U", "postgres",
                          "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1"], input=sql)

    def redis_rows(name, sql):
        # CSV goes to stdout and logs to stderr; --limit 0 sends the SQL unwrapped.
        result = sl(name, "conns", "exec", "REDIS_ADBC", "--output", "csv", "--limit", "0", sql)
        return list(csv.reader(io.StringIO(result.stdout)))[1:]

    def transfer(name, source, stream, target, output, *extra, extra_env=None):
        sl(name, "run", "--src-conn", source, "--src-stream", stream,
           "--tgt-conn", target, "--tgt-object", output, *extra, extra_env=extra_env)

    def check(name, action, requires=()):
        missing = [r for r in requires if outcomes.get(r, {}).get("status") != "passed"]
        if missing:
            outcomes[name] = {"status": "not-run", "requires": missing}
            return
        try:
            detail = action()
        except Unavailable as exc:
            outcomes[name] = {"status": "unavailable", "detail": str(exc)}
        except Exception as exc:
            outcomes[name] = {"status": "failed", "detail": str(exc)}
        else:
            outcomes[name] = {"status": "passed"}
            if detail is not None:
                outcomes[name]["detail"] = detail

    def compare(name, table):
        result = pg(name, f"""
          SELECT (SELECT count(*) FROM {schema}.source),
                 (SELECT count(*) FROM {schema}.{table}),
                 (SELECT count(*) FROM (
                   (SELECT * FROM {schema}.source EXCEPT ALL SELECT * FROM {schema}.{table})
                   UNION ALL
                   (SELECT * FROM {schema}.{table} EXCEPT ALL SELECT * FROM {schema}.source)
                 ) differences);
        """)
        observed = result.stdout.strip()
        if observed != f"{ROWS}|{ROWS}|0":
            raise RuntimeError(f"row/value mismatch (source|copy|differences): {observed}")

    def redis_count(name, table):
        rows = redis_rows(name, f"SELECT COUNT(*) AS row_count FROM {schema}.{table}")
        if len(rows) != 1 or len(rows[0]) != 1:
            raise RuntimeError(f"unexpected count result: {rows}")
        return int(rows[0][0])

    def expect_redis_count(name, table, expected):
        def action():
            count = redis_count(name, table)
            if count != expected:
                raise RuntimeError(f"{schema}.{table} has {count} rows, expected {expected}")
        return action

    def redis_tables(name):
        return [row[0] for row in redis_rows(name,
            "SELECT table_name FROM information_schema.tables "
            f"WHERE table_schema = '{schema}' ORDER BY table_name")]

    def pg_types(name, table):
        result = pg(name, f"""
          SELECT {TYPE_FIELDS} FROM information_schema.columns
          WHERE table_schema = '{schema}' AND table_name = '{table}' ORDER BY ordinal_position;
        """)
        return [line.split("|") for line in result.stdout.splitlines() if line]

    def ddl_difference(name, table):
        # An observation: values are compared exactly elsewhere; DDL is reported.
        def action():
            source, copy = pg_types(name + "-source", "source"), pg_types(name, table)
            return {"columns": copy,
                    "differences": [{"source": s, "copy": c}
                                    for s, c in zip(source, copy) if s != c]
                                   + [{"source": s, "copy": None} for s in source[len(copy):]]
                                   + [{"source": None, "copy": c} for c in copy[len(source):]]}
        return action

    def discover():
        # Discovery is part of the official Sling build, not its public source.
        result = sl("discover-columns", "conns", "discover", "REDIS_ADBC",
                    "--pattern", f"{schema}.staged", "--columns", check=False)
        if "please use the official build" in result.stdout + result.stderr:
            raise Unavailable("this Sling build omits conns discover")
        if result.returncode:
            raise RuntimeError("discover-columns failed; inspect its private log")
        missing = [c for c in COLUMNS
                   if not re.search(rf"\|\s*{re.escape(c)}\s*\|", result.stdout)]
        if missing:
            raise RuntimeError(f"discovered columns omit {missing}")

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
        digest = hashlib.sha256(Path(sling).read_bytes()).hexdigest()
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
          CREATE TABLE {schema}.empty_source (LIKE {schema}.source);
        """)

        check("redis-create", lambda: redis_exec("redis-create",
            f"CREATE SCHEMA IF NOT EXISTS {schema}",
            f"CREATE TABLE {schema}.inbound {TABLE_DDL}",
            f"CREATE TABLE {schema}.empty_inbound {TABLE_DDL}"))
        check("empty-arrow-query", lambda: redis_exec("empty-arrow-query",
            f"SELECT label FROM {schema}.inbound WHERE 1=0"), ["redis-create"])

        # Sling's default full-refresh: stage, then create or replace the target.
        # The first run creates the table; the second replaces an existing one.
        for phase in ("create", "replace"):
            check(f"staging-{phase}", lambda phase=phase: transfer(
                f"staging-{phase}", "PG_SLING", f"{schema}.source",
                "REDIS_ADBC", f"{schema}.staged", "--mode", "full-refresh"),
                ["redis-create"] if phase == "create" else ["staging-create"])
            check(f"staging-{phase}-export", lambda phase=phase: transfer(
                f"staging-{phase}-export", "REDIS_ADBC", f"{schema}.staged",
                "PG_SLING", f"{schema}.staged_roundtrip", "--mode", "full-refresh"),
                [f"staging-{phase}"])
            check(f"staging-{phase}-comparison", lambda phase=phase: compare(
                f"staging-{phase}-comparison", "staged_roundtrip"), [f"staging-{phase}-export"])
            if phase == "create":
                check("discover-columns", discover, ["staging-create"])
                check("ddl-redis-staged", lambda: redis_rows("ddl-redis-staged",
                    f"SELECT {TYPE_FIELDS} FROM information_schema.columns "
                    f"WHERE table_schema = '{schema}' AND table_name = 'staged' "
                    "ORDER BY ordinal_position"), ["staging-create"])

        # A precreated table, truncate and direct insert (the Sling 1.6.6 path).
        for iteration in range(2):
            check(f"direct-pg-to-redis-{iteration}", lambda i=iteration: transfer(
                f"direct-pg-to-redis-{i}", "PG_SLING", f"{schema}.source",
                "REDIS_ADBC", f"{schema}.inbound", "--mode", "truncate",
                "--tgt-options", '{"direct_insert":true}'), ["redis-create"])
            check(f"redis-to-pg-{iteration}", lambda i=iteration: transfer(
                f"redis-to-pg-{i}", "REDIS_ADBC", f"{schema}.inbound",
                "PG_SLING", f"{schema}.roundtrip", "--mode", "full-refresh"),
                [f"direct-pg-to-redis-{iteration}"])
            check(f"exact-comparison-{iteration}", lambda i=iteration: compare(
                f"exact-comparison-{i}", "roundtrip"), [f"redis-to-pg-{iteration}"])
        check("ddl-postgres-roundtrip", ddl_difference("ddl-postgres-roundtrip", "roundtrip"),
              ["redis-to-pg-1"])
        check("ddl-postgres-staged-roundtrip",
              ddl_difference("ddl-postgres-staged-roundtrip", "staged_roundtrip"),
              ["staging-replace-export"])

        # Empty sources. By default Sling creates no target for an empty stream;
        # SLING_ALLOW_EMPTY makes it create the empty table through staging.
        allow_empty = {"SLING_ALLOW_EMPTY": "TRUE"}
        check("empty-source-default", lambda: transfer(
            "empty-source-default", "PG_SLING", f"{schema}.empty_source",
            "REDIS_ADBC", f"{schema}.empty_default", "--mode", "full-refresh"), ["redis-create"])

        def no_empty_default_table():
            tables = [t for t in redis_tables("empty-source-default-tables")
                      if t.startswith("empty_default")]
            if tables:
                raise RuntimeError(f"empty default transfer left tables {tables}")
        check("empty-source-default-tables", no_empty_default_table, ["empty-source-default"])
        check("empty-source-staging", lambda: transfer(
            "empty-source-staging", "PG_SLING", f"{schema}.empty_source",
            "REDIS_ADBC", f"{schema}.empty_staged", "--mode", "full-refresh",
            extra_env=allow_empty), ["redis-create"])
        check("empty-source-staging-count",
              expect_redis_count("empty-source-staging-count", "empty_staged", 0),
              ["empty-source-staging"])
        check("ddl-redis-empty-staged", lambda: redis_rows("ddl-redis-empty-staged",
            f"SELECT {TYPE_FIELDS} FROM information_schema.columns "
            f"WHERE table_schema = '{schema}' AND table_name = 'empty_staged' "
            "ORDER BY ordinal_position"), ["empty-source-staging"])
        check("empty-source-truncate", lambda: transfer(
            "empty-source-truncate", "PG_SLING", f"{schema}.empty_source",
            "REDIS_ADBC", f"{schema}.inbound", "--mode", "truncate",
            "--tgt-options", '{"direct_insert":true}'), ["exact-comparison-1"])
        check("empty-source-truncate-count",
              expect_redis_count("empty-source-truncate-count", "inbound", 0),
              ["empty-source-truncate"])
        # An empty Redis table exported to a new PostgreSQL table.
        check("empty-redis-export", lambda: transfer(
            "empty-redis-export", "REDIS_ADBC", f"{schema}.empty_inbound",
            "PG_SLING", f"{schema}.empty_roundtrip", "--mode", "full-refresh",
            extra_env=allow_empty), ["redis-create"])

        def empty_postgres_count():
            count = pg("empty-redis-export-count",
                       f"SELECT count(*) FROM {schema}.empty_roundtrip;").stdout.strip()
            if count != "0":
                raise RuntimeError(f"{schema}.empty_roundtrip has {count} rows, expected 0")
        check("empty-redis-export-count", empty_postgres_count, ["empty-redis-export"])

        # Sling drops its staging tables, through the driver, after each transfer.
        def no_staging_tables():
            tables = redis_tables("staging-tables-removed")
            if any(t.endswith("_tmp") for t in tables):
                raise RuntimeError(f"staging tables remain: {tables}")
            return tables
        check("staging-tables-removed", no_staging_tables, ["redis-create"])

        results = {"sling": version, "sling_sha256": digest, "postgresql": pg_version,
                   "rows": ROWS, "columns": len(COLUMNS), "outcomes": outcomes}
        (work / "results.json").write_text(json.dumps(results, indent=2, ensure_ascii=False) + "\n")
        print(json.dumps(results, indent=2, ensure_ascii=False))
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
    failed = [n for n, o in outcomes.items() if o["status"] != "passed"]
    if failed:
        sys.exit("Sling cases failed or did not run: " + ", ".join(failed))


if __name__ == "__main__":
    main()
