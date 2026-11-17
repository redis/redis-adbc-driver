#!/usr/bin/env python3
"""Verify unmanaged replace refusals with actual owned Software ACL principals."""
import argparse
import json
import os
from pathlib import Path
import secrets
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
    uri = os.environ["REDIS_URI"]
    endpoint = urlsplit(uri)
    if (endpoint.scheme != "rediss" or endpoint.hostname not in ("localhost", "127.0.0.1")
            or endpoint.port != args.software_data_port):
        raise RuntimeError("requires the owned local strict-TLS data endpoint")
    # The standard redis-up fixture installs its proxy CA only. Bootstrap trust
    # for this explicitly owned localhost management endpoint, as setup does.
    with tempfile.TemporaryDirectory(prefix="redis-adbc-replace-acl-") as private:
        folder = Path(private)
        folder.chmod(0o700)
        proxy_ca = Path(os.environ["SSL_CERT_FILE"]).read_text()
        (folder / "ca.crt").write_text(proxy_ca)
        api = SoftwareAPI(args.software_api_port)
        databases = api.call("GET", "bdbs")
        if len(databases) != 1 or databases[0]["name"] != "adbc-test":
            raise RuntimeError("requires the owned single-database Software fixture")
        database = databases[0]
        effective = api.call("GET", f"bdbs/{database['uid']}?fields=status,query_performance_factor,search,sched_policy,conns")
        if database.get("eviction_policy") != "noeviction" or not qpf_4x_ready(effective):
            raise RuntimeError("requires verified noeviction and effective active 4x QPF")
        (folder / "software.json").write_text(json.dumps({
            "api_port": args.software_api_port, "bdb_uid": database["uid"]}))
        (folder / "admin.uri").write_text(uri)
        (folder / "admin.uri").chmod(0o600)
        principals = SoftwarePrincipals(folder, api=api)
        try:
            env = dict(os.environ, REDIS_SNAPSHOT_URI=uri)
            for denied in ("del", "hset", "ft.dropindex", "ft.create"):
                name = "replace-preflight-" + uuid.uuid4().hex + "@example.com"
                rules = ["+@all", "-" + denied, "~*"]
                if denied == "hset":
                    # Catalog reader leases need HSET while source HASH writes
                    # must remain denied. ACL selectors authorize only metadata.
                    rules.append("(+hset ~adbc:{meta}:*)")
                principals.create(name, secrets.token_urlsafe(32), rules)
                key = "REDIS_UNMANAGED_REPLACE_" + denied.upper().replace(".", "_") + "_ACL_URI"
                env[key] = principals.people[name]["uri"]
            env.pop("REDIS_URI", None)
            subprocess.run(["go", "test", "-count=1", "-p", "1", "-v", "-run",
                            "^TestUnmanagedArrowReplacePermissionPreflight$", "."],
                           cwd=Path(__file__).resolve().parents[1] / "go", env=env, check=True)
        finally:
            principals.cleanup()
    print("Unmanaged replacement permission refusals verified; temporary Software principals removed.")


if __name__ == "__main__":
    main()
