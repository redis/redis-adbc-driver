#!/usr/bin/env python3
"""Verify schema ACL isolation on an explicitly owned local Software fixture.

Creates temporary SQL objects and Software API principals, checks positive
metadata/public-row access and negative other-schema access, then removes only
those owned resources. No credentials, endpoint identifiers or row data are
emitted. A failed isolation check exits nonzero; it must not become a skip.
"""

import argparse
import json
import os
from pathlib import Path
import secrets
import shutil
import tempfile
from urllib.parse import urlsplit
import uuid

import adbc_driver_manager.dbapi
import redis

from software_partition_tls import SoftwareAPI, SoftwarePrincipals, qpf_4x_ready


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--driver", type=Path, required=True)
    parser.add_argument("--software-api-port", type=int, required=True)
    parser.add_argument("--software-data-port", type=int, required=True)
    parser.add_argument("--owned-software-fixture", action="store_true", required=True)
    args = parser.parse_args()
    ca = Path(os.environ["SSL_CERT_FILE"])
    uri = os.environ["REDIS_URI"]
    endpoint = urlsplit(uri)
    if (
        endpoint.scheme != "rediss"
        or endpoint.hostname not in ("localhost", "127.0.0.1")
        or endpoint.port != args.software_data_port
    ):
        raise RuntimeError("requires the owned local strict-TLS data endpoint")
    api = SoftwareAPI(args.software_api_port, ca)
    databases = api.call("GET", "bdbs")
    if len(databases) != 1 or databases[0]["name"] != "adbc-test":
        raise RuntimeError("requires the owned single-database Software fixture")
    database = databases[0]
    if database.get("eviction_policy") != "noeviction":
        raise RuntimeError("requires verified noeviction")
    effective = api.call(
        "GET",
        f"bdbs/{database['uid']}?fields=status,query_performance_factor,search,sched_policy,conns",
    )
    if not qpf_4x_ready(effective):
        raise RuntimeError("requires active verified 4x QPF")
    isolated = False
    with tempfile.TemporaryDirectory(prefix="redis-adbc-acl-scope-") as private:
        folder = Path(private)
        (folder / "software.json").write_text(
            json.dumps({"api_port": args.software_api_port, "bdb_uid": database["uid"]})
        )
        shutil.copyfile(ca, folder / "ca.crt")
        admin_uri = folder / "admin.uri"
        admin_uri.write_text(uri)
        admin_uri.chmod(0o600)
        principals = SoftwarePrincipals(folder)
        suffix = uuid.uuid4().hex
        schema = "it_acl_scope_" + suffix
        public = "it_acl_scope_" + suffix
        name = "acl-scope-" + suffix + "@example.com"
        created_reserved = False
        with adbc_driver_manager.dbapi.connect(
            driver=str(args.driver.resolve()), db_kwargs={"uri": uri}, autocommit=True
        ) as conn, conn.cursor() as cur:
            try:
                cur.execute(f"CREATE SCHEMA {schema}")
                cur.execute(f"CREATE TABLE {schema}.rows(v INTEGER)")
                cur.execute(f"INSERT INTO {schema}.rows VALUES(314159)")
                cur.execute(f"CREATE TABLE public.{public}(v INTEGER)")
                cur.execute(f"INSERT INTO public.{public} VALUES(271828)")
                cur.execute(
                    f"SELECT key_prefix,index_name FROM information_schema.tables WHERE table_schema='{schema}' AND table_name='rows'"
                )
                protected_prefix, protected_index = cur.fetchone()
                cur.execute(
                    "SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name='adbc'"
                )
                if cur.fetchone()[0] == 0:
                    cur.execute("CREATE SCHEMA adbc")
                    created_reserved = True
                cur.execute(f"CREATE TABLE adbc.{public}(v INTEGER)")
                cur.execute(f"INSERT INTO adbc.{public} VALUES(161803)")
                cur.execute(
                    f"SELECT key_prefix,index_name FROM information_schema.tables WHERE table_schema='adbc' AND table_name='{public}'"
                )
                reserved_prefix, reserved_index = cur.fetchone()
                cur.execute(
                    f"SELECT key_prefix,index_name FROM information_schema.tables WHERE table_schema='public' AND table_name='{public}'"
                )
                public_prefix, public_index = cur.fetchone()
                principals.create(
                    name,
                    secrets.token_urlsafe(32),
                    [
                        "+ping",
                        "+hgetall",
                        "+ft.search",
                        "+ft.aggregate",
                        "%R~adbc:*",
                        "%R~public:*",
                    ],
                )
                with principals.client(name) as limited:
                    if (
                        limited.hgetall("adbc:{meta}:snapshot:catalog").get("format")
                        is None
                    ):
                        raise RuntimeError("metadata positive control failed")
                    if limited.hgetall(public_prefix + "1").get("v") != "271828":
                        raise RuntimeError("public-row positive control failed")
                    search = limited.execute_command(
                        "FT.SEARCH", public_index, "*", "NOCONTENT", "LIMIT", 0, 1
                    )
                    aggregate = limited.execute_command(
                        "FT.AGGREGATE", public_index, "*", "LOAD", 2, "@__key", "@v"
                    )
                    if not isinstance(search, dict) or not isinstance(aggregate, dict):
                        raise RuntimeError("ACL probe requires RESP3 Search replies")
                    if (
                        search.get("total_results") != 1
                        or aggregate.get("total_results") != 1
                    ):
                        raise RuntimeError("public Search positive controls failed")
                    denied = 0
                    for prefix, index in (
                        (protected_prefix, protected_index),
                        (reserved_prefix, reserved_index),
                    ):
                        for command in (
                            ("HGETALL", prefix + "1"),
                            ("FT.SEARCH", index, "*", "NOCONTENT", "LIMIT", 0, 1),
                            ("FT.AGGREGATE", index, "*", "LOAD", 2, "@__key", "@v"),
                        ):
                            try:
                                limited.execute_command(*command)
                            except redis.exceptions.NoPermissionError:
                                denied += 1
                    isolated = denied == 6

            finally:
                try:
                    principals.cleanup()
                finally:
                    try:
                        cur.execute(f"DROP TABLE IF EXISTS public.{public}")
                    finally:
                        try:
                            cur.execute(f"DROP SCHEMA IF EXISTS {schema} CASCADE")
                        finally:
                            cur.execute(f"DROP TABLE IF EXISTS adbc.{public}")
                            if created_reserved:
                                cur.execute("DROP SCHEMA adbc")
    print(
        json.dumps(
            {
                "software": True,
                "qpf": 4,
                "noeviction": True,
                "strict_tls": True,
                "schema_isolated": isolated,
                "protected_schemas_checked": 2,
                "row_read_commands_checked": 3,
                "owned_resources_removed": True,
            }
        )
    )
    if not isolated:
        raise SystemExit(
            "FAIL: public-schema ACL patterns also grant another schema's snapshot HASHes"
        )


if __name__ == "__main__":
    main()
