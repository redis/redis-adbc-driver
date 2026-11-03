#!/usr/bin/env python3
"""Run row-batch permission regressions with an owned Software API principal.

Creates a temporary principal with DEL/HSET allowed and ZREM/ZADD denied. Credentials and
infrastructure identifiers stay private. The Go test clears the dedicated DB.
"""

import argparse
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import tempfile
from urllib.parse import urlsplit
import uuid

from software_partition_tls import SoftwareAPI, SoftwarePrincipals, qpf_4x_ready


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--software-api-port", type=int, required=True)
    parser.add_argument("--software-data-port", type=int, required=True)
    parser.add_argument("--owned-software-fixture", action="store_true", required=True)
    args = parser.parse_args()
    ca = Path(os.environ["SSL_CERT_FILE"])
    uri = os.environ["REDIS_URI"]
    endpoint = urlsplit(uri)
    if (endpoint.scheme != "rediss" or endpoint.hostname not in ("localhost", "127.0.0.1")
            or endpoint.port != args.software_data_port):
        raise RuntimeError("requires the owned local strict-TLS data endpoint")
    api = SoftwareAPI(args.software_api_port, ca)
    databases = api.call("GET", "bdbs")
    if len(databases) != 1 or databases[0]["name"] != "adbc-test":
        raise RuntimeError("requires the owned single-database Software fixture")
    database = databases[0]
    if database.get("eviction_policy") != "noeviction":
        raise RuntimeError("requires verified noeviction")
    effective = api.call("GET", f"bdbs/{database['uid']}?fields=status,query_performance_factor,search,sched_policy,conns")
    if not qpf_4x_ready(effective):
        raise RuntimeError("requires active verified 4x QPF")
    with tempfile.TemporaryDirectory(prefix="redis-adbc-gc-acl-") as private:
        folder = Path(private)
        (folder / "software.json").write_text(json.dumps({
            "api_port": args.software_api_port, "bdb_uid": database["uid"]}))
        shutil.copyfile(ca, folder / "ca.crt")
        admin_uri = folder / "admin.uri"
        admin_uri.write_text(uri)
        admin_uri.chmod(0o600)
        principals = SoftwarePrincipals(folder)
        name = "gc-preflight-" + uuid.uuid4().hex + "@example.com"
        try:
            principals.create(name, secrets.token_urlsafe(32), [
                "+ping", "+eval", "+hget", "+hexists", "+zscore", "+zcard", "+del",
                "+hset", "+exists", "+type", "+time", "~*"])
            env = dict(os.environ, REDIS_SNAPSHOT_URI=uri,
                       REDIS_SNAPSHOT_GC_ACL_URI=principals.people[name]["uri"],
                       REDIS_SNAPSHOT_WRITE_ACL_URI=principals.people[name]["uri"])
            env.pop("REDIS_URI", None)
            subprocess.run(["go", "test", "-race", "-count=1", "-v", "-run",
                            "^TestSnapshot(GCBatch|InsertBatch)PermissionPreflight$", "."],
                           cwd=Path(__file__).resolve().parents[1] / "go", env=env, check=True)
        finally:
            principals.cleanup()
    print("Row-batch permission preflights verified; temporary Software principal removed.")


if __name__ == "__main__":
    main()
