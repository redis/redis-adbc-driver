#!/usr/bin/env python3
"""Validate a staged reader-lease model on an owned Software fixture.

The production driver/catalog format is unchanged. Only shadow keys under a
unique reader-model prefix and temporary API principals are created. Existing
catalog bytes must stay identical. Credentials and operational IDs stay private.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import tempfile
from urllib.parse import urlsplit
import uuid

import redis
from software_partition_tls import SoftwareAPI, SoftwarePrincipals, qpf_4x_ready


def rules(prefix, user):
    namespace = hashlib.sha256(user.encode()).hexdigest()
    return [
        "+ping", "+hget", "+hgetall", "+get", "+exists", "+smembers",
        "+scard", "+sismember", "+time", "+eval_ro", "%R~" + prefix + "*",
        # EVAL's conservative key specification needs RW access to its declared
        # keys. Actual Lua commands still require their separate selectors.
        "(+eval ~" + prefix + "*)",
        "(+hset +hdel ~" + prefix + "*:reader:" + namespace + ")",
        "(+sadd ~" + prefix + "*:namespaces)",
    ]


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
        raise RuntimeError("requires the owned local strict-TLS endpoint")
    api = SoftwareAPI(args.software_api_port, ca)
    databases = api.call("GET", "bdbs")
    if len(databases) != 1 or databases[0]["name"] != "adbc-test":
        raise RuntimeError("requires the owned single-database fixture")
    database = databases[0]
    effective = api.call("GET", f"bdbs/{database['uid']}?fields=status,query_performance_factor,search,sched_policy,conns")
    if database.get("eviction_policy") != "noeviction" or not qpf_4x_ready(effective):
        raise RuntimeError("requires verified active 4x QPF and noeviction")
    prefix = "adbc:{meta}:snapshot:reader-model:" + uuid.uuid4().hex + ":"
    with redis.Redis.from_url(uri, ssl_ca_certs=str(ca), decode_responses=False) as admin:
        before = admin.dump("adbc:{meta}:snapshot:catalog")
        with tempfile.TemporaryDirectory(prefix="redis-adbc-reader-model-") as directory:
            folder = Path(directory)
            (folder / "software.json").write_text(json.dumps({"api_port": args.software_api_port, "bdb_uid": database["uid"]}))
            shutil.copyfile(ca, folder / "ca.crt")
            (folder / "admin.uri").write_text(uri)
            (folder / "admin.uri").chmod(0o600)
            principals = SoftwarePrincipals(folder)
            reader, peer = ("reader-model-" + uuid.uuid4().hex + "@example.com" for _ in range(2))
            try:
                for person in (reader, peer):
                    principals.create(person, secrets.token_urlsafe(32), rules(prefix, person))
                env = dict(os.environ, REDIS_READER_MODEL_PREFIX=prefix,
                           REDIS_READER_MODEL_URI=principals.people[reader]["uri"],
                           REDIS_READER_MODEL_PEER_URI=principals.people[peer]["uri"])
                env.pop("REDIS_SNAPSHOT_URI", None)
                env.pop("REDIS_SNAPSHOT_CLUSTER_URI", None)
                subprocess.run(["go", "test", "-race", "-json", "-count=1", "-p", "1", "-run",
                                "^TestSnapshotReaderLeaseModel", "."],
                               cwd=Path(__file__).resolve().parents[1] / "go", env=env, check=True)
            finally:
                try:
                    principals.cleanup()
                finally:
                    remaining = list(admin.scan_iter(match=prefix + "*", count=100))
                    if remaining:
                        admin.delete(*remaining)
                    if admin.dump("adbc:{meta}:snapshot:catalog") != before:
                        raise RuntimeError("staged model changed the production catalog")
                    if list(admin.scan_iter(match=prefix + "*", count=100)):
                        raise RuntimeError("shadow model keys remained after cleanup")
    print(json.dumps({"software": True, "qpf": 4, "strict_tls": True,
                      "catalog_unchanged": True, "owned_resources_removed": True,
                      "model_only": True}, sort_keys=True))


if __name__ == "__main__":
    main()
