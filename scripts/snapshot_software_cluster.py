#!/usr/bin/env python3
"""Owned three-node Software fixture; operational state stays in a private directory.

Requires `docker compose -f go/compose.snapshot-cluster.yaml up -d --wait` with
COMPOSE_PROJECT_NAME set explicitly. Never points at a shared or paid deployment.
"""

import argparse
import json
import os
import re
from pathlib import Path
import subprocess
import sys
import time
from urllib.parse import quote, urlencode

from software_partition_tls import SoftwareAPI, qpf_4x_ready

ROOT = Path(__file__).resolve().parents[1]


def docker(*args, capture=True):
    return (
        subprocess.check_output(["docker", *args], text=True)
        if capture
        else subprocess.check_call(["docker", *args])
    )


def wait(check, what, seconds=180):
    end = time.monotonic() + seconds
    while True:
        try:
            result = check()
            if result:
                return result
        except (OSError, RuntimeError):
            pass
        if time.monotonic() >= end:
            raise TimeoutError(what)
        time.sleep(0.5)


def owned_nodes(project):
    nodes = []
    for service in ("node1", "node2", "node3"):
        ids = docker(
            "ps",
            "-aq",
            "--filter",
            f"label=com.docker.compose.project={project}",
            "--filter",
            f"label=com.docker.compose.service={service}",
        ).split()
        if len(ids) != 1:
            raise ValueError("fixture must have exactly one owned container per node")
        node = json.loads(docker("inspect", ids[0]))[0]
        if node["Config"]["Labels"].get("codex.fixture") != "snapshot-software-cluster":
            raise ValueError("fixture ownership label mismatch")
        networks = list(node["NetworkSettings"]["Networks"].values())
        if len(networks) != 1:
            raise ValueError("fixture must have one private network")
        binding = node["NetworkSettings"]["Ports"]["9443/tcp"]
        if len(binding) != 1 or binding[0]["HostIp"] != "127.0.0.1":
            raise ValueError("fixture management must bind only loopback")
        nodes.append(
            {
                "container": ids[0],
                "ip": networks[0]["IPAddress"],
                "api_port": int(binding[0]["HostPort"]),
            }
        )
    return nodes


def prepare(args):
    args.persistence = args.persistence or "aof"
    directory = args.directory.resolve()
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    if (directory / "fixture.json").exists():
        raise ValueError("use a fresh private fixture directory")
    if directory.stat().st_mode & 0o077:
        raise ValueError("fixture directory must be private (mode 0700)")
    nodes = owned_nodes(args.project)
    user = os.environ.get("SOFTWARE_ADMIN_USER", "admin@example.com")
    password = os.environ.get("SOFTWARE_ADMIN_PASSWORD", "adbc-software-test")
    database_password = os.environ.get("SOFTWARE_DB_PASSWORD", "adbc-enterprise-test")
    credentials = {"username": user, "password": password}
    for i, node in enumerate(nodes):
        api = SoftwareAPI(node["api_port"])

        def bootstrap_state():
            return api.call("GET", "bootstrap").get("bootstrap_status", {}).get("state")

        state = wait(bootstrap_state, "management bootstrap unavailable")
        if state == "completed":
            continue
        if state != "idle":
            raise ValueError(
                "only idle or already-owned completed Software nodes may be used"
            )
        body = {
            "action": "create_cluster" if i == 0 else "join_cluster",
            "credentials": credentials,
            "node": {"identity": {"addr": node["ip"]}},
            "cluster": (
                {"name": "snapshot-test.local"}
                if i == 0
                else {"nodes": [nodes[0]["ip"]]}
            ),
        }
        api.call("POST", "bootstrap/" + body["action"], body)
        wait(
            lambda: bootstrap_state() == "completed",
            "Software bootstrap did not complete",
        )
        print(f"Software node {i + 1} bootstrapped", flush=True)

    sans = ["DNS:localhost", "DNS:node1", "DNS:node2", "DNS:node3", "IP:127.0.0.1"] + [
        "IP:" + n["ip"] for n in nodes
    ]
    subprocess.run(
        [
            "openssl",
            "req",
            "-x509",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-days",
            "7",
            "-keyout",
            str(directory / "server.key"),
            "-out",
            str(directory / "ca.pem"),
            "-subj",
            "/CN=snapshot-test.local",
            "-addext",
            "subjectAltName=" + ",".join(sans),
            "-addext",
            "basicConstraints=critical,CA:TRUE",
        ],
        check=True,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    certificate = (directory / "ca.pem").read_text()
    key = (directory / "server.key").read_text()
    SoftwareAPI(nodes[0]["api_port"]).call(
        "PUT",
        "cluster/certificates",
        {
            "certificates": [
                {"name": name, "certificate": certificate, "key": key}
                for name in ("api", "proxy")
            ]
        },
    )
    api = SoftwareAPI(nodes[0]["api_port"], directory / "ca.pem")
    api.call("GET", "cluster")
    if args.license_file:
        license_file = args.license_file.resolve()
        if license_file.stat().st_mode & 0o077:
            raise ValueError("license file must be private (mode 0600)")
        api.call("PUT", "license", {"license": license_file.read_text()})
        print("Private fixture license installed", flush=True)
    existing = api.call("GET", "bdbs")
    body = database_config(database_password, persistence=args.persistence)
    if existing:
        if (
            not args.resume
            or len(existing) != 1
            or any(
                existing[0].get(k) != body[k]
                for k in (
                    "name",
                    "shards_count",
                    "replication",
                    "oss_cluster",
                    "data_persistence",
                    "port",
                )
            )
            or not persistence_matches(existing[0], args.persistence)
        ):
            raise ValueError("owned fixture contains an unexpected database")
        database = existing[0]
    else:
        database = api.call("POST", "bdbs", body)
    uid = database["uid"]
    wait(
        lambda: qpf_4x_ready(
            api.call(
                "GET",
                f"bdbs/{uid}?fields=status,query_performance_factor,search,sched_policy,conns",
            )
        ),
        "effective 4x QPF not ready",
    )
    database = api.call("GET", f"bdbs/{uid}")
    shards = wait(lambda: ready_shards(api, uid, 3, True), "three primaries not ready")
    if not (
        database["oss_cluster"]
        and database["replication"]
        and persistence_matches(database, args.persistence)
        and database["eviction_policy"] == "noeviction"
    ):
        raise ValueError("fixture durability/routing policy mismatch")
    for node in nodes:
        docker(
            "cp", str(directory / "ca.pem"), node["container"] + ":/tmp/snapshot-ca.pem"
        )
    config = {
        "project": args.project,
        "nodes": nodes,
        "bdb_uid": uid,
        "database_password": database_password,
        "database_version": database["version"],
        "qpf_verified": True,
        "durability_verified": True,
        "primaries": 3,
        "replication": True,
        "persistence": args.persistence,
    }
    checkpoint_management(config)
    (directory / "fixture.json").write_text(json.dumps(config))
    os.chmod(directory / "fixture.json", 0o600)
    print(
        f"Verified 3-node / 3-primary / 3-replica Software Cluster API, TLS, {args.persistence} persistence and active 4x QPF",
        flush=True,
    )


def persistence_policy(mode):
    if mode == "aof":
        return {"data_persistence": "aof", "aof_policy": "appendfsync-always"}
    if mode == "snapshot":
        # Test-only frequent checkpoints; the orchestrator dirties every primary.
        # Persist both roles so recovery cannot depend on an old AOF copy.
        return {"data_persistence": "snapshot", "snapshot_policy": [{"secs": 5, "writes": 1}], "master_persistence": True}
    raise ValueError("unsupported fixture persistence mode")


def persistence_matches(database, mode):
    return all(database.get(key) == value for key, value in persistence_policy(mode).items())


def database_config(database_password, replication=True, persistence="aof"):
    return {
        "name": "snapshot-cluster-test",
        "type": "redis",
        "redis_version": "8.6",
        "port": 12000,
        "memory_size": 1610612736,
        "replication": replication,
        "sharding": True,
        "shards_count": 3,
        "shards_placement": "sparse",
        "oss_cluster": True,
        "proxy_policy": "all-nodes",
        "hash_slots_policy": "16k",
        "shard_key_regex": [{"regex": r".*\{(?<tag>.*)\}.*"}, {"regex": "(?<tag>.*)"}],
        "module_list": [{"module_name": "search", "module_args": ""}],
        "query_performance_factor": {"active": True, "scaling_factor": 4},
        "tls_mode": "enabled",
        "enforce_client_authentication": "disabled",
        "authentication_redis_pass": database_password,
        "eviction_policy": "noeviction",
        **persistence_policy(persistence),
    }


def ready_shards(api, uid, primaries, replication):
    shards = api.call("GET", f"bdbs/{uid}/shards")
    if len(shards) != primaries * (2 if replication else 1) or any(
        s["status"] != "active"
        or s.get("actual_role") != s["role"]
        or s.get("loading", {}).get("status") != "idle"
        for s in shards
    ):
        return None
    if sum(s["role"] == "master" for s in shards) != primaries:
        return None
    return shards


def load_fixture(args):
    directory = args.directory.resolve()
    config = json.loads((directory / "fixture.json").read_text())
    if (
        config["project"] != args.project
        or owned_nodes(args.project) != config["nodes"]
    ):
        raise ValueError("owned fixture identity changed")
    if getattr(args, "persistence", None) not in (None, config.get("persistence", "aof")):
        raise ValueError("requested persistence does not match the owned fixture")
    api = SoftwareAPI(config["nodes"][0]["api_port"], directory / "ca.pem")
    database = api.call("GET", f"bdbs/{config['bdb_uid']}")
    if not (
        database["oss_cluster"]
        and database["eviction_policy"] == "noeviction"
        and persistence_matches(database, config.get("persistence", "aof"))
        and database["shards_count"] == config["primaries"]
        and database["replication"] == config["replication"]
    ):
        raise ValueError("fixture routing/durability policy changed")
    qpf = api.call(
        "GET",
        f"bdbs/{config['bdb_uid']}?fields=status,query_performance_factor,search,sched_policy,conns",
    )
    if not qpf_4x_ready(qpf) or not ready_shards(
        api, config["bdb_uid"], config["primaries"], config["replication"]
    ):
        raise ValueError("fixture QPF/shard readiness verification failed")
    return config, api


def client_container(project):
    ids = docker(
        "ps",
        "-q",
        "--filter",
        f"label=com.docker.compose.project={project}",
        "--filter",
        "label=com.docker.compose.service=client",
    ).split()
    if len(ids) != 1:
        raise ValueError("owned independent client container is missing")
    info = json.loads(docker("inspect", ids[0]))[0]
    if info["Config"]["Labels"].get("codex.fixture") != "snapshot-software-client":
        raise ValueError("independent client ownership mismatch")
    return ids[0]


def test_command(args, config):
    client = client_container(args.project)
    api = SoftwareAPI(config["nodes"][0]["api_port"], args.directory.resolve() / "ca.pem")
    shards = ready_shards(api, config["bdb_uid"], config["primaries"], config["replication"])
    if not shards:
        raise ValueError("owned shard slot ranges are unavailable")
    # Software can route two physical primaries through one node endpoint
    # after promotion. RDB probes must dirty each physical shard, not merely
    # each distinct advertised address.
    primary_slots = [
        [[int(a), int(b)] for a, b in (part.split("-") for part in shard["assigned_slots"].split(","))]
        for shard in shards if shard["role"] == "master"
    ]
    docker(
        "cp", str(args.test_binary.resolve()), client + ":/tmp/snapshot-cluster.test"
    )
    docker(
        "cp", str(args.directory.resolve() / "ca.pem"), client + ":/tmp/snapshot-ca.pem"
    )
    uri = (
        "rediss://default:"
        + quote(config["database_password"], safe="")
        + "@"
        + config["nodes"][0]["ip"]
        + ":12000/0"
    )
    inspection_uri = uri
    uri += "?" + urlencode([("addr", node["ip"] + ":12000") for node in config["nodes"][1:]])
    return [
        "docker",
        "exec",
        "-e",
        "REDIS_SNAPSHOT_CLUSTER_URI=" + uri,
        "-e",
        # Generic partition cleanup uses adminURI and discovers Cluster slots.
        # Keep its URI free of the driver's repeated addr query parameters.
        "REDIS_ADMIN_URI=" + inspection_uri,
        "-e",
        "REDIS_SNAPSHOT_CLUSTER_PRIMARIES=" + str(config["primaries"]),
        "-e",
        "REDIS_SNAPSHOT_CLUSTER_SHARD_SLOTS=" + json.dumps(primary_slots),
        "-e",
        "REDIS_RETAINED_CONTROL_PERSISTENCE=guaranteed",
        "-e",
        "SSL_CERT_FILE=/tmp/snapshot-ca.pem",
        client,
        "/tmp/snapshot-cluster.test",
        "-test.v",
        "-test.timeout",
        "10m",
        "-test.run",
        args.test_pattern,
    ]


def run_tests(args):
    config, _ = load_fixture(args)
    path = args.directory.resolve() / "cluster-tests.log"
    with path.open("w") as output:
        result = subprocess.run(
            test_command(args, config), stdout=output, stderr=subprocess.STDOUT
        )
    print("Cluster API regression exit:", result.returncode, flush=True)
    if result.returncode:
        raise SystemExit(result.returncode)


def search_registry(args, config, client):
    def command(node, *words):
        result = subprocess.run(
            [
                "docker",
                "exec",
                "-e",
                "REDISCLI_AUTH=" + config["database_password"],
                client,
                "redis-cli",
                "--tls",
                "--cacert",
                "/tmp/snapshot-ca.pem",
                "-h",
                node["ip"],
                "-p",
                "12000",
                "-3",
                "--json",
                *words,
            ],
            capture_output=True,
            text=True,
            timeout=10,
        )
        if result.returncode:
            raise RuntimeError("Search readiness connection failed")
        try:
            return json.loads(re.sub(r"(?<=:)nan(?=[,}])", "NaN", result.stdout))
        except ValueError:
            raise RuntimeError("Search readiness reply unavailable") from None

    registries = []
    for node in config["nodes"]:
        slots = command(node, "CLUSTER", "SLOTS")
        if not isinstance(slots, list) or not slots:
            raise RuntimeError("Cluster API routing is not ready")
        indexes = command(node, "FT._LIST")
        if not isinstance(indexes, list):
            raise RuntimeError("Search registry recovering")
        for index in indexes:
            info = command(node, "FT.INFO", index)
            if (
                not isinstance(info, dict)
                or float(info.get("percent_indexed", 0)) < 1
                or int(info.get("indexing", 1)) != 0
            ):
                raise RuntimeError("Search index recovering")
        registries.append(set(indexes))
    if any(x != registries[0] for x in registries):
        raise RuntimeError("Search registries disagree")
    return registries[0]


def checkpoint_management(config):
    # Persist only Software's cluster configuration, on every local CCS. This
    # never saves or changes the SQL database: its rows/catalog rely on the
    # separately verified persistence policy/checkpoint.
    # Abrupt full-quorum loss must not restore an older BDB UID or routing mode.
    for node in config["nodes"]:
        result = docker(
            "exec", node["container"], "ccs-cli", "--quiet", "--local", "SAVE"
        )
        if result.strip() != "OK":
            raise RuntimeError("owned fixture management checkpoint failed")


def redis_info_fields(info):
    """Flatten Software's per-shard INFO sections, never logging their values."""
    if not isinstance(info, dict):
        raise ValueError("per-shard Redis INFO is unavailable")
    fields = {}
    for key, value in info.items():
        if isinstance(value, dict):
            fields.update(redis_info_fields(value))
        else:
            fields[key] = value
    return fields


def rdb_checkpoint_ready(shards, after):
    if not shards:
        return False
    for shard in shards:
        info = redis_info_fields(shard.get("redis_info"))
        try:
            if (int(info["aof_enabled"]) != 0
                    or int(info["rdb_bgsave_in_progress"]) != 0
                    or info["rdb_last_bgsave_status"] != "ok"
                    or int(info["rdb_last_save_time"]) < after):
                return False
        except (KeyError, TypeError, ValueError):
            return False
    return True


def rdb_checkpoint_shards(config, api):
    ready = ready_shards(api, config["bdb_uid"], config["primaries"], True)
    if not ready:
        return None
    keys = ("aof_enabled", "rdb_bgsave_in_progress", "rdb_last_bgsave_status", "rdb_last_save_time")
    # Default shard objects deliberately omit INFO values. Request only the
    # documented persistence fields, then retain exactly our verified shards.
    reported = api.call("GET", "shards?" + urlencode([("extra_info_keys", key) for key in keys]))
    selected = [s for s in reported if s["bdb_uid"] == config["bdb_uid"]]
    if {s["uid"] for s in selected} != {s["uid"] for s in ready}:
        return None
    return selected


def checkpoint_rdb(config, api, command, output):
    # Require every shard's completed RDB to be newer than the paused test's final writes.
    # This is separate from saving Software management CCS configuration.
    checkpoint = command[:]
    checkpoint[2:2] = ["-e", "REDIS_SNAPSHOT_RDB_CHECKPOINT=1"]
    checkpoint[-1] = "^TestSnapshotClusterRDBCheckpointWrites$"
    result = subprocess.run(checkpoint, stdout=output, stderr=subprocess.STDOUT)
    if result.returncode:
        # The command contains private connection environment values.
        raise RuntimeError("owned RDB checkpoint writes failed")
    # INFO timestamps have whole-second precision. A checkpoint in the same
    # second can precede the writes, even if it satisfies a pre-write bound.
    # Require a strictly later second than the acknowledged final probe write.
    after = int(time.time()) + 1
    current = rdb_checkpoint_shards(config, api)
    if rdb_checkpoint_ready(current, after + 3600):
        raise ValueError("RDB gate accepted a future checkpoint")
    wait(lambda: rdb_checkpoint_ready(
        rdb_checkpoint_shards(config, api), after),
        "fresh completed RDB checkpoint with AOF disabled was not verified", seconds=90)
    print("Fresh completed RDB checkpoints and disabled AOF verified on every shard", flush=True)


def recovery_tests(args):
    import binascii

    config, api = load_fixture(args)
    client = client_container(args.project)
    command = test_command(args, config)
    chaos = "/tmp/snapshot-chaos"
    docker("exec", client, "mkdir", "-p", chaos)
    docker("exec", client, "rm", "-f", chaos + "/ready", chaos + "/continue")
    command[2:2] = ["-e", "REDIS_SNAPSHOT_CHAOS_DIR=" + chaos]
    if config.get("persistence", "aof") == "snapshot" and args.action == "restart":
        command[2:2] = ["-e", "REDIS_SNAPSHOT_RDB_REQUIRED=1"]
    path = args.directory.resolve() / (args.action + "-tests.log")
    with path.open("w") as output:
        process = subprocess.Popen(command, stdout=output, stderr=subprocess.STDOUT)
        try:

            def ready():
                if process.poll() is not None:
                    raise ValueError(
                        "recovery test exited before interruption; inspect private log"
                    )
                return (
                    subprocess.run(
                        ["docker", "exec", client, "test", "-f", chaos + "/ready"],
                        stdout=subprocess.DEVNULL,
                        stderr=subprocess.DEVNULL,
                    ).returncode
                    == 0
                )

            wait(ready, "recovery test did not become ready", seconds=120)
            shards = ready_shards(api, config["bdb_uid"], config["primaries"], True)
            expected_indexes = search_registry(args, config, client)
            if args.action == "failover":
                slot = binascii.crc_hqx(b"meta", 0) % 16384
                owner = next(
                    x
                    for x in shards
                    if x["role"] == "master"
                    and any(
                        int(a) <= slot <= int(b)
                        for a, b in (
                            part.split("-") for part in x["assigned_slots"].split(",")
                        )
                    )
                )
                api.call(
                    "POST",
                    f"shards/{owner['uid']}/actions/failover",
                    {"force_rebind": True},
                )

                def promoted():
                    current = ready_shards(
                        api, config["bdb_uid"], config["primaries"], True
                    )
                    return current and any(
                        x["uid"] == owner["uid"] and x["role"] == "slave"
                        for x in current
                    )

                wait(promoted, "catalog primary promotion did not finish")
                print("Catalog primary failover verified", flush=True)
            else:
                if config.get("persistence", "aof") == "snapshot":
                    checkpoint_rdb(config, api, command, output)
                checkpoint_management(config)
                containers = [n["container"] for n in config["nodes"]]
                docker("kill", *containers)
                if any(
                    json.loads(docker("inspect", c))[0]["State"]["Running"]
                    for c in containers
                ):
                    raise ValueError("database containers did not all stop")
                docker("start", *containers)
                wait(
                    lambda: ready_shards(
                        api, config["bdb_uid"], config["primaries"], True
                    ),
                    "persistence recovery did not finish",
                    seconds=240,
                )
                print("Abrupt all-node restart and shard recovery verified", flush=True)
            wait(
                lambda: qpf_4x_ready(
                    api.call(
                        "GET",
                        f"bdbs/{config['bdb_uid']}?fields=status,query_performance_factor,search,sched_policy,conns",
                    )
                ),
                "QPF recovery did not finish",
            )
            recovered = api.call("GET", f"bdbs/{config['bdb_uid']}")
            if not recovered["oss_cluster"] or not persistence_matches(
                    recovered, config.get("persistence", "aof")):
                raise ValueError("Cluster API/persistence fixture policy did not persist")
            wait(
                lambda: search_registry(args, config, client) == expected_indexes,
                "distributed Search recovery did not finish",
                seconds=90,
            )
            docker("exec", client, "touch", chaos + "/continue")
            code = process.wait(timeout=120)
        finally:
            if process.poll() is None:
                subprocess.run(
                    [
                        "docker",
                        "exec",
                        client,
                        "pkill",
                        "-f",
                        r"^/tmp/snapshot-cluster.test",
                    ],
                    stdout=subprocess.DEVNULL,
                    stderr=subprocess.DEVNULL,
                )
                process.terminate()
                process.wait(timeout=10)
    print(args.action + " regression exit:", code, flush=True)
    if code:
        raise SystemExit(code)


def proxy_regressions(args):
    config, api = load_fixture(args)
    api.call("PUT", f"bdbs/{config['bdb_uid']}", {"oss_cluster": False})
    wait(
        lambda: not api.call("GET", f"bdbs/{config['bdb_uid']}")["oss_cluster"],
        "proxy mode did not activate",
    )
    wait(
        lambda: qpf_4x_ready(
            api.call(
                "GET",
                f"bdbs/{config['bdb_uid']}?fields=status,query_performance_factor,search,sched_policy,conns",
            )
        ),
        "proxy QPF readiness",
    )
    wait(lambda: ready_shards(api, config["bdb_uid"], 3, True), "proxy shard readiness")
    info = json.loads(docker("inspect", config["nodes"][0]["container"]))[0]
    binding = info["NetworkSettings"]["Ports"]["12000/tcp"]
    if len(binding) != 1 or binding[0]["HostIp"] != "127.0.0.1":
        raise ValueError("proxy must bind only loopback")
    env = os.environ.copy()
    env.pop("REDIS_URI", None)
    env.pop("REDIS_SNAPSHOT_CLUSTER_URI", None)
    env["REDIS_SNAPSHOT_URI"] = (
        "rediss://default:"
        + quote(config["database_password"], safe="")
        + "@localhost:"
        + binding[0]["HostPort"]
        + "/0"
    )
    env["SSL_CERT_FILE"] = str(args.directory.resolve() / "ca.pem")
    env["REDIS_RETAINED_CONTROL_PERSISTENCE"] = "guaranteed"
    commands = [
        (
            "snapshot-race",
            ["go", "test", "-race", "-count=1", "-v", "-run", "^TestSnapshot", "."],
        ),
        ("native-build", ["make", "build"]),
        (
            "native-snapshot",
            [os.path.abspath(args.python), "validation/snapshot_validation.py"],
        ),
    ]
    if args.proxy_checks == "native":
        commands = commands[2:]
    # This is the final fixture phase. Do not toggle Cluster API back on and
    # immediately crash the database: management reconfiguration is asynchronous.
    # A later Cluster API action deliberately fails load_fixture's policy check.
    for name, command in commands:
        with (args.directory.resolve() / (name + ".log")).open("w") as output:
            result = subprocess.run(
                command,
                cwd=ROOT / "go",
                env=env,
                stdout=output,
                stderr=subprocess.STDOUT,
            )
        print(name + " exit:", result.returncode, flush=True)
        if result.returncode:
            raise SystemExit(result.returncode)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--directory", type=Path, required=True)
    parser.add_argument("--project", required=True)
    parser.add_argument(
        "--license-file",
        type=Path,
        help="private license file for six replicated shard processes; never committed or logged",
    )
    parser.add_argument(
        "--resume",
        action="store_true",
        help="resume this exact owned partially initialized fixture",
    )
    parser.add_argument(
        "--action",
        choices=("prepare", "test", "failover", "restart", "proxy"),
        default="prepare",
    )
    parser.add_argument("--proxy-checks", choices=("all", "native"), default="all")
    parser.add_argument("--persistence", choices=("aof", "snapshot"),
                        help="Owned fixture persistence policy; snapshot is a test-only frequent RDB policy")
    parser.add_argument(
        "--python",
        type=Path,
        default=Path(sys.executable),
        help="Python with native ADBC validation dependencies",
    )
    parser.add_argument(
        "--test-binary", type=Path, default=ROOT / "go/build/snapshot-cluster.test"
    )
    parser.add_argument("--test-pattern", default="^TestSnapshotCluster")
    args = parser.parse_args()
    (
        prepare
        if args.action == "prepare"
        else (
            run_tests
            if args.action == "test"
            else proxy_regressions if args.action == "proxy" else recovery_tests
        )
    )(args)
