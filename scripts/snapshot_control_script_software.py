#!/usr/bin/env python3
"""Verify control-script compatibility with an EVAL-only Software operator.

Destructive Go snapshot fixtures run only on an explicitly owned local test
database. The temporary full operator excludes EVALSHA; this is not a restricted
SQL reader. Credentials and operational identifiers remain in private files/env.
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

from software_partition_tls import SoftwareAPI, SoftwarePrincipals, qpf_4x_ready, wait_for


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--software-api-port", type=int, required=True)
    parser.add_argument("--software-data-port", type=int, required=True)
    parser.add_argument("--owned-software-fixture", action="store_true", required=True)
    args = parser.parse_args()
    ca, uri = Path(os.environ["SSL_CERT_FILE"]), os.environ["REDIS_URI"]
    endpoint = urlsplit(uri)
    if (endpoint.scheme != "rediss" or endpoint.hostname not in ("localhost", "127.0.0.1")
            or endpoint.port != args.software_data_port):
        raise RuntimeError("requires the explicitly owned local strict-TLS endpoint")
    api = SoftwareAPI(args.software_api_port, ca)
    databases = api.call("GET", "bdbs")
    if len(databases) != 1 or databases[0]["name"] != "adbc-test":
        raise RuntimeError("requires the owned single-database fixture")
    database = databases[0]
    path = f"bdbs/{database['uid']}"

    def verify():
        current = api.call("GET", path)
        if current.get("eviction_policy") != "noeviction":
            raise RuntimeError("requires verified noeviction")
        # ACL mapping reconfiguration can briefly unset effective routing even
        # after authentication becomes ready. Never start Go before it settles.
        wait_for(lambda: qpf_4x_ready(api.call("GET", path +
                 "?fields=status,query_performance_factor,search,sched_policy,conns")),
                 "active/effective 4x QPF did not settle")

    verify()
    with tempfile.TemporaryDirectory(prefix="redis-adbc-script-operator-") as directory:
        folder = Path(directory)
        (folder / "software.json").write_text(json.dumps({"api_port": args.software_api_port,
                                                       "bdb_uid": database["uid"]}))
        shutil.copyfile(ca, folder / "ca.crt")
        (folder / "admin.uri").write_text(uri)
        (folder / "admin.uri").chmod(0o600)
        principals = SoftwarePrincipals(folder)
        operator = "script-operator-" + uuid.uuid4().hex + "@example.com"
        try:
            principals.create(operator, secrets.token_urlsafe(32), ["~*", "+@all", "-evalsha"])
            verify()
            env = dict(os.environ, REDIS_SNAPSHOT_URI=uri,
                       REDIS_SCRIPT_EVAL_ONLY_URI=principals.people[operator]["uri"],
                       REDIS_RETAINED_CONTROL_PERSISTENCE="guaranteed")
            subprocess.run(["go", "test", "-race", "-json", "-count=1", "-p", "1",
                            "-timeout=2m", "-run", "^TestSnapshotControlScriptEvalOnlySoftware$", "."],
                           cwd=Path(__file__).resolve().parents[1] / "go", env=env, check=True)
        finally:
            principals.cleanup()
            verify()
    print(json.dumps({"software": True, "qpf": 4, "strict_tls": True,
                      "eval_only_operator_verified": True, "owned_principal_removed": True}))


if __name__ == "__main__":
    main()
