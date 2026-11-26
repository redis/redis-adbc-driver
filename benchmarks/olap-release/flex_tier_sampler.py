"""Reference Flex RAM/flash object sampler for the harness's flex_tier_sampler hook.

    flex_tier_sampler.py /private/management.json

Redis Software 8.8 Flex does not report the RAM/flash object split through the
database endpoint, so the harness can run a caller-owned command just before
timing (see README "Per-query diagnostics"). This one reads the database's
latest 1-second interval from the Redis Software management API,
GET /v1/bdbs/stats/last/<bdb_uid>, and prints one JSON object with the hook's
field names. run.py never calls the management API; the coordinator runs this
as a separate process only when the private profile names it.

The private JSON file holds `url` (https://host:port), `username`, `password`,
`bdb_uid`, optional `ca_file`, and optional `verify_tls` (default true; false is
accepted only for a localhost API, such as a client in the server's network
namespace with the cluster's self-signed certificate). Failures print only the
exception type (and HTTP status) to stderr and exit 1. Standard library only.
"""

import base64
import json
import math
from pathlib import Path
import ssl
import sys
import urllib.error
import urllib.parse
import urllib.request


TIMEOUT_SECONDS = 20
LOCAL_HOSTS = frozenset({"localhost", "127.0.0.1", "::1"})
METRICS = {"bigstore_objs_ram": "ram_objects", "bigstore_objs_flash": "flash_objects",
           "bigstore_objs_clean": "ram_clean_objects", "bigstore_objs_dirty": "ram_dirty_objects",
           "no_of_keys": "keys", "used_memory": "used_memory_bytes", "used_bigstore": "used_flash_bytes"}
TIMES = {"stime": "interval_start_utc", "etime": "interval_end_utc"}


def request(config):
    """The stats request and its TLS context; refuses unsafe settings before connecting."""
    if not isinstance(config, dict):
        raise ValueError("management config must be a JSON object")
    url = config.get("url")
    parts = urllib.parse.urlsplit(url) if isinstance(url, str) else None
    if (parts is None or parts.scheme != "https" or not parts.hostname or parts.username or parts.password
            or parts.path not in ("", "/") or parts.query or parts.fragment):
        raise ValueError("url must be https://host:port, without credentials or a path")
    uid = config.get("bdb_uid")
    if type(uid) is not int or uid < 1:
        raise ValueError("bdb_uid must be a positive integer")
    user, password = config.get("username"), config.get("password")
    if not (isinstance(user, str) and user and isinstance(password, str)):
        raise ValueError("username and password must be strings")
    verify = config.get("verify_tls", True)
    if type(verify) is not bool:
        raise ValueError("verify_tls must be a boolean")
    context = ssl.create_default_context(cafile=config.get("ca_file"))
    if not verify:
        if parts.hostname not in LOCAL_HOSTS:
            raise ValueError("verify_tls false is accepted only for a localhost management API")
        context.check_hostname = False
        context.verify_mode = ssl.CERT_NONE
    token = base64.b64encode(f"{user}:{password}".encode()).decode()
    stats = urllib.request.Request(f"{url.rstrip('/')}/v1/bdbs/stats/last/{uid}", method="GET",
                                   headers={"Authorization": f"Basic {token}", "Accept": "application/json"})
    return stats, context


def _count(value):
    if type(value) in (int, float) and math.isfinite(value) and value >= 0 and value == int(value):
        return int(value)
    return None


def sample(config, opener=urllib.request.urlopen):
    stats, context = request(config)
    with opener(stats, context=context, timeout=TIMEOUT_SECONDS) as reply:
        value = json.loads(reply.read())
    interval = value.get(str(config["bdb_uid"])) if isinstance(value, dict) else None
    if not isinstance(interval, dict):
        raise ValueError("the reply has no statistics for bdb_uid")
    out = {"source": "management_api_bdb_stats"}
    for key, name in METRICS.items():
        if _count(interval.get(key)) is not None:
            out[name] = _count(interval[key])
    for key, name in TIMES.items():
        if isinstance(interval.get(key), str):
            out[name] = interval[key]
    return out


def main(argv):
    if len(argv) != 2:
        print("usage: flex_tier_sampler.py <private management.json>", file=sys.stderr)
        return 2
    try:
        value = sample(json.loads(Path(argv[1]).read_text()))
    except Exception as exc:  # never the URL, user or password
        status = f" {exc.code}" if isinstance(exc, urllib.error.HTTPError) else ""
        print(f"flex_tier_sampler: {type(exc).__name__}{status}", file=sys.stderr)
        return 1
    print(json.dumps(value))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
