"""Refuse Flex timings on Search indexes the disk GC has not yet compacted.

On Redis Software Flex (search_on_bigstore), a new Search index keeps one disk
entry per document per field until the disk GC's first cycle merges them. Until
then every NUMERIC term scans all of a field's entries: 0.6 s per term against
6-11 ms afterwards at 6.0M documents. The first cycle came 5.9-9.4 minutes after
FT.CREATE on the measured fixture, and clients cannot start it on Redis Software
(see docs/sdlc/changes/flex-index-compaction). So the harness waits, bounded, and
refuses to time an index whose FT.INFO does not show a completed cycle with every
NUMERIC field's disk_num_keys below num_docs.

Indexes are mapped to loaded tables by their fields: each driver row index
contains __-prefixed internal fields and the table's indexed columns. Public
provenance records counts and column names only, never index names or prefixes.

A profile may opt into exempting small indexes (at most 10,000 documents) that
have not been compacted: on 2026-11-26 the SSB date_ and supplier indexes, each
loaded in one Arrow batch, got no first cycle in 26 and 19.5 minutes. An exempt
index needs its caller-supplied FT.CREATE time and records one point-term
FT.PROFILE to show its uncompacted cost. Larger indexes are never exempt.
"""

import datetime
import json
import os
from pathlib import Path
import time


WAIT_SECONDS = 900
POLL_SECONDS = 15
# search-fork-gc-clean-threshold: the disk GC runs after this many changes, so
# a smaller index may never get a cycle (and its full scan is that small).
GC_THRESHOLD_DEFAULT = 100
# The largest index a profile may exempt: an uncompacted term then scans at most
# 10,000 entries, about 1 ms at the measured 95-100 ns per entry.
EXEMPT_MAX_DOCS_LIMIT = 10_000
EXEMPT_OPTION = "flex_compaction_exempt_max_docs"
CREATED_FILE = "flex-index-created.json"


class CompactionGateError(RuntimeError):
    """The indexes were not compacted in time; .state is the last observation."""

    def __init__(self, message, state):
        super().__init__(message)
        self.state = state


def _json(path):
    return json.loads(path.read_text()) if path.exists() else {}


def flex_profile(private):
    """A Flex profile: the driver's Flex option or verified search_on_bigstore."""
    private = Path(private)
    options = _json(private / "connection.json").get("db_kwargs", {})
    flag = str(options.get("adbc.redis.flex", "")).strip().lower() == "true"
    return flag or _json(private / "server.json").get("search_on_bigstore") is True


def gc_threshold(private):
    value = _json(Path(private) / "server.json").get("search", {}).get("search-fork-gc-clean-threshold")
    return value if type(value) is int and value > 0 else GC_THRESHOLD_DEFAULT


def exempt_max_docs(private):
    """The opt-in exemption limit from connection.json; 0 (strict) when absent."""
    config = _json(Path(private) / "connection.json")
    if EXEMPT_OPTION not in config:
        return 0
    value = config[EXEMPT_OPTION]
    if type(value) is not int or not 1 <= value <= EXEMPT_MAX_DOCS_LIMIT:
        raise ValueError(f"{EXEMPT_OPTION} must be an integer from 1 to {EXEMPT_MAX_DOCS_LIMIT}")
    return value


def created_times(private):
    """Caller-supplied FT.CREATE times (UTC, from the shard log) by table."""
    return _json(Path(private) / CREATED_FILE)


def utc_now():
    return datetime.datetime.now(datetime.timezone.utc)


def _utc(text):
    return datetime.datetime.fromisoformat(text.replace("Z", "+00:00"))


def _text(value):
    return value.decode() if isinstance(value, bytes) else value


def _mapping(value):
    """A RESP3 map, or the flat key/value list RESP2 returns for one."""
    if isinstance(value, dict):
        return {_text(k): v for k, v in value.items()}
    if isinstance(value, (list, tuple)) and len(value) % 2 == 0:
        return {_text(value[i]): value[i + 1] for i in range(0, len(value), 2)}
    raise ValueError(f"FT.INFO: expected a map, got {type(value).__name__}")


def _count(value):
    return int(float(_text(value)))


def _fields(info):
    """(identifier, type) of each index attribute."""
    return [(_text(a["identifier"]), _text(a["type"]).upper()) for a in map(_mapping, info.get("attributes", []))]


def columns(info):
    """The index's public column fields; driver-internal fields start with __."""
    return {name for name, _ in _fields(_mapping(info)) if not name.startswith("__")}


def index_state(info, gc_threshold=GC_THRESHOLD_DEFAULT, exempt_max_docs=0):
    """Compaction state of one index from its FT.INFO reply."""
    info = _mapping(info)
    docs = _count(info["num_docs"])
    cycles = _count(_mapping(info["gc_stats"])["total_cycles"])
    stats = {_text(s["identifier"]): s.get("disk_num_keys") for s in map(_mapping, info.get("field statistics", []))}
    numeric = sorted(name for name, kind in _fields(info) if kind == "NUMERIC")
    keys = {name: None if stats.get(name) is None else _count(stats[name]) for name in numeric}
    problems = []
    if docs < gc_threshold:
        kind = "below_gc_threshold"
    else:
        if cycles < 1:
            problems.append("no completed disk GC cycle")
        if not numeric:
            problems.append("no NUMERIC field to verify")
        # A field no document has (the driver's deletion marker on a table
        # without deletes) reports no disk_num_keys: it has no entries to scan.
        # An index where no NUMERIC field reports them is not a Flex index.
        elif all(keys[name] is None for name in numeric):
            problems.append("no disk_num_keys on any NUMERIC field")
        full = [name for name in numeric if keys[name] is not None and keys[name] >= docs]
        if full:
            problems.append("disk_num_keys not below num_docs for NUMERIC " + ", ".join(full))
        kind = "uncompacted" if problems else "compacted"
    state = {"state": kind, "num_docs": docs, "gc_total_cycles": cycles,
             "max_numeric_disk_num_keys": max((v for v in keys.values() if v is not None), default=None),
             "numeric_disk_num_keys": keys, "problems": problems}
    if kind == "uncompacted" and docs <= exempt_max_docs:
        state.update(state="exempt_uncompacted", uncompacted_reasons=problems, problems=[])
    return state


def evaluate(infos, tables, gc_threshold=GC_THRESHOLD_DEFAULT, exempt_max_docs=0):
    """Every loaded table needs at least one index, and all of its indexes ready.

    tables maps each loaded table to its column names. An index whose public
    fields belong to no loaded table is counted, not checked; one whose fields
    fit several tables cannot be attributed and blocks the gate.
    """
    owned = {table: [] for table in tables}
    unmatched, problems, exempt = 0, [], []
    for info in infos:
        fields = columns(info)
        owners = sorted(t for t, cols in tables.items() if fields and fields <= set(cols))
        if len(owners) > 1:
            problems.append("ambiguous index: its fields fit tables " + ", ".join(owners))
        elif owners:
            state = index_state(info, gc_threshold, exempt_max_docs)
            owned[owners[0]].append(state)
            if state["state"] == "exempt_uncompacted":
                exempt.append((owners[0], _text(_mapping(info)["index_name"]), state))
        else:
            unmatched += 1
    result = {}
    for table, states in owned.items():
        found = [p for s in states for p in s["problems"]] if states else ["no Search index found"]
        result[table] = {"ready": not found, "indexes": states}
        problems.extend(f"{table}: {p}" for p in found)
    state = {"ready": not problems, "tables": result, "unmatched_indexes": unmatched,
             "gc_threshold": gc_threshold, "problems": problems, "_exempt": exempt}
    if exempt_max_docs:
        small = lambda i: gc_threshold <= i["num_docs"] <= exempt_max_docs
        state["exemption"] = {
            "max_docs": exempt_max_docs,
            "applied_to": sorted({table for table, _, _ in exempt}),
            # Small enough to be exempt, but compacted: the exemption was dropped.
            "not_needed": sorted(t for t, v in result.items()
                                 if any(small(i) and i["state"] == "compacted" for i in v["indexes"])),
        }
    return state


def wait_until_compacted(fetch, tables, gc_threshold=GC_THRESHOLD_DEFAULT, wait_seconds=None,
                         poll_seconds=None, clock=time.monotonic, sleep=time.sleep, progress=None,
                         exempt_max_docs=0, inspect=None):
    """Poll fetch() (FT.INFO replies) until evaluate() is ready; refuse after wait_seconds.

    inspect(table, index_name, state) records an exempt index's evidence and
    returns its problems; it runs only once everything else is ready."""
    wait_seconds = WAIT_SECONDS if wait_seconds is None else wait_seconds
    poll_seconds = POLL_SECONDS if poll_seconds is None else poll_seconds
    started, polls = clock(), 0
    while True:
        state = evaluate(fetch(), tables, gc_threshold, exempt_max_docs)
        exempt = state.pop("_exempt")
        if state["ready"] and exempt:
            for table, name, index in exempt:
                found = inspect(table, name, index) if inspect else ["exemption evidence unavailable"]
                if found:
                    state["tables"][table]["ready"] = state["ready"] = False
                    state["problems"].extend(f"{table}: {p}" for p in found)
        polls += 1
        waited = clock() - started
        state.update(polls=polls, waited_seconds=round(waited, 1), wait_limit_seconds=wait_seconds)
        if state["ready"]:
            return state
        if waited >= wait_seconds:
            raise CompactionGateError(
                f"Flex Search indexes not compacted after {round(waited, 1)} s; refusing to time: "
                + "; ".join(state["problems"]), state)
        if progress:
            progress(state)
        sleep(min(poll_seconds, wait_seconds - waited))


def redis_client(config):
    """A RESP3 redis-py client for the profile's URI, trusting SSL_CERT_FILE."""
    import redis

    options = {"protocol": 3, "decode_responses": True, "socket_timeout": 60}
    if config["uri"].startswith("rediss://") and os.environ.get("SSL_CERT_FILE"):
        options["ssl_ca_certs"] = os.environ["SSL_CERT_FILE"]
    return redis.Redis.from_url(config["uri"], **options)


def redis_fetch(config):
    """A RESP3 client and a function returning FT.INFO for every Search index."""
    import redis

    client = redis_client(config)

    def fetch():
        infos = []
        for name in client.execute_command("FT._LIST"):
            try:
                infos.append(client.execute_command("FT.INFO", name))
            except redis.ResponseError as exc:
                # Dropped between FT._LIST and FT.INFO (a reclaimed private output).
                if "not found" in str(exc).lower() or "unknown index" in str(exc).lower():
                    continue
                raise
        return infos

    return client, fetch


def _escape(field):
    return "".join(c if c.isalnum() or c == "_" else "\\" + c for c in field)


def point_term_profile(client, index, field):
    """FT.PROFILE of one point term (an existing value) on a NUMERIC field, per shard."""
    found = _mapping(client.execute_command("FT.SEARCH", index, "*", "LIMIT", 0, 1, "RETURN", 1, field, "DIALECT", 2))
    value = _text(_mapping(_mapping(found["results"][0])["extra_attributes"])[field])
    float(value)
    reply = _mapping(client.execute_command("FT.PROFILE", index, "SEARCH", "QUERY",
                                            f"@{_escape(field)}:[{value} {value}]",
                                            "NOCONTENT", "LIMIT", 0, 0, "DIALECT", 2))
    shards = []
    for shard in _mapping(reply["Profile"])["Shards"]:
        shard = _mapping(shard)
        iterator = _mapping(shard["Iterators profile"])
        shards.append({"field": field, "total_ms": float(shard["Total profile time"]),
                       "iterator": _text(iterator["Type"]), "iterator_ms": float(iterator["Time"]),
                       "estimated_matches": _count(iterator.get("Estimated number of matches", 0))})
    return shards


def gate(private, tables, wait_seconds=None, progress=None):
    """Wait for every loaded table's indexes to be compacted; return the observation."""
    config = json.loads((Path(private) / "connection.json").read_text())
    limit = exempt_max_docs(private)
    created = created_times(private) if limit else {}
    client, fetch = redis_fetch(config)

    def inspect(table, index, state):
        problems = []
        if not isinstance(created.get(table), str):
            problems.append(f"exemption needs its FT.CREATE time in {CREATED_FILE}")
        else:
            state["ft_create_utc"] = created[table]
            state["minutes_since_ft_create"] = round((utc_now() - _utc(created[table])).total_seconds() / 60, 1)
        field = next((c for c in tables[table] if c in state["numeric_disk_num_keys"]), None)
        if field is None:
            problems.append("exemption needs a NUMERIC column to profile")
        else:
            try:
                state["point_term_profile"] = point_term_profile(client, index, field)
            except Exception as exc:  # recorded as a refusal, never as evidence
                problems.append(f"exemption point-term FT.PROFILE failed ({type(exc).__name__})")
        return problems

    try:
        state = wait_until_compacted(fetch, tables, gc_threshold(private), wait_seconds=wait_seconds,
                                     progress=progress, exempt_max_docs=limit, inspect=inspect)
    finally:
        client.close()
    state["columns"] = {table: list(cols) for table, cols in tables.items()}
    return state


def summary(state):
    """A compact progress line: per table, cycles and the largest NUMERIC key count per index."""
    return {"waited_seconds": state.get("waited_seconds"), "ready": state["ready"],
            "tables": {t: [{"num_docs": i["num_docs"], "gc_total_cycles": i["gc_total_cycles"],
                            "max_numeric_disk_num_keys": max((v for v in i["numeric_disk_num_keys"].values()
                                                              if v is not None), default=None),
                            "state": i["state"]} for i in v["indexes"]]
                       for t, v in state["tables"].items()}}
