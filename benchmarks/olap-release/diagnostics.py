"""Public per-query diagnostics: a whitelisted driver sidecar and the Flex object split.

The driver's adbc.redis.execution_diagnostics_file sidecar (docs/execution-diagnostics.md)
carries plans with SQL literals, column names and index queries. Only the fields
listed here reach a public record: numbers, booleans, driver identifiers, and
counts or presence flags in place of literal-bearing values.

flex_tiers() samples INFO over RESP3 for the RAM/flash object split. On Redis
Software 8.8 Flex the database endpoint does not report it (2026-11-26): its
proxy answers no Bigredis section and keeps only keys/expires/avg_ttl on db0.
The sample then says so, and never estimates a split.

This module imports neither engine; redis-py is imported only when sampling.
"""

import datetime
import json
import math
from pathlib import Path
import re


OPTION = "adbc.redis.execution_diagnostics_file"
WARMUP_SIDECAR = "{engine}-warmup-diagnostics.json"
DUCKDB_ADBC_UNAVAILABLE = ("not recorded: the diagnostics file is a statement option, adbc_scanner opens its "
                           "own statements, and the driver rejects it as an ATTACH (database) option")

IDENTIFIER = re.compile(r"[a-z][a-z0-9_]{0,63}")
STATUSES = frozenset({"running", "execute_returned", "execute_failed"})
METRICS = ("version", "execute_elapsed_seconds", "worker_grant", "processed_rows", "matched_rows",
           "redis_commands", "redis_round_trip_worker_seconds", "socket_read_worker_seconds",
           "connection_read_bytes", "connection_written_bytes", "process_allocated_bytes_during_execute",
           "process_heap_bytes")
# Driver-chosen booleans, sizes, counts and input positions; never SQL values.
PLAN_VALUES = frozenset({
    "hash_scan", "index_sort", "search_result_cap", "aggregate_result_cap", "late_projection", "tie_sort",
    "workers", "dimension_count", "output_page_rows", "exact_directory_ranges", "chunk_count",
    "chunk_row_target", "source_cache_per_worker", "snowflake", "parallel_dimensions",
    "complete_flex_candidates", "fact", "dimension_rows", "dimension_cap", "spilled_dimension",
    "spill_partitions", "order", "checked_integer_inputs", "partition_rows", "server_source_tuples",
    "client_compact_tuples", "compact_probe_max_seconds", "partial_groups", "count_groups",
    "encoded_group_key_mask", "live_unmanaged", "input_slots", "bound_residual", "cohort_members",
    "physical_fields", "visible_index_queries"})
PLAN_IDENTIFIERS = ("kind", "reason", "source")
# Literal-bearing lists are recorded only by their length.
PLAN_COUNTS = {"columns": "column_count", "sort": "sort_key_count", "input_columns": "input_column_count",
               "group_expressions": "group_expression_count"}
# The join-order plan's per-input entries are already positions, row counts and flags.
JOIN_INPUT_FIELDS = frozenset({"input", "source_rows", "candidate_rows", "in_memory", "snapshot_overlay",
                               "index_filter", "residual_filter", "local_filter_count",
                               "compiled_scalar_filter", "large_published_native"})
JOIN_SCOPE_FIELDS = frozenset({"snapshot", "pin", "shared_pin", "store_match", "maintenance", "operation",
                               "write", "retained", "partition_executor"})
AGGREGATE_METRICS = ("workers", "partition_attempts", "group_limit_retries", "row_fallbacks", "returned_tuples",
                     "represented_rows", "logical_value_bytes", "redis_reply_seconds",
                     "partition_worker_seconds", "merge_wall_seconds", "spill_written_bytes",
                     "row_scan_partitions", "spill_read_bytes", "spill_read_worker_seconds",
                     "spill_write_worker_seconds", "spill_sort_worker_seconds",
                     "partition_fetch_worker_seconds", "merge_elapsed_seconds", "compact_tuple_worker_seconds")
MAX_LIST = 256


def _number(value):
    return type(value) in (int, float) and math.isfinite(value)


def _scalar(value):
    return type(value) is bool or _number(value)


def _identifier(value):
    return isinstance(value, str) and IDENTIFIER.fullmatch(value) is not None


def _values(value):
    """A plan value: a boolean or finite number, or a short list of them."""
    if _scalar(value):
        return True
    return isinstance(value, list) and len(value) <= MAX_LIST and all(_scalar(v) for v in value)


def _fields(value, allowed):
    return {k: v for k, v in value.items() if k in allowed and _scalar(v)} if isinstance(value, dict) else None


def sanitize_plan(plan):
    clean = {k: plan[k] for k in PLAN_IDENTIFIERS if _identifier(plan.get(k))}
    clean.update({k: v for k, v in plan.items() if k in PLAN_VALUES and _values(v)})
    if "search_query" in plan:
        clean["index_filter"] = isinstance(plan["search_query"], str) and plan["search_query"] not in ("", "*")
    if "residual" in plan:
        clean["residual"] = plan["residual"] not in (None, False)
    if "pushed_limit" in plan:
        clean["limit_pushed"] = plan["pushed_limit"] is not None
    for key, name in PLAN_COUNTS.items():
        if isinstance(plan.get(key), list):
            clean[name] = len(plan[key])
    if isinstance(plan.get("inputs"), list) and len(plan["inputs"]) <= MAX_LIST:
        inputs = [_fields(item, JOIN_INPUT_FIELDS) for item in plan["inputs"]]
        if all(item is not None for item in inputs):
            clean["inputs"] = inputs
    scope = _fields(plan.get("scope"), JOIN_SCOPE_FIELDS)
    if scope is not None:
        clean["scope"] = scope
    return clean


def sanitize(snapshot):
    """The public copy of one driver diagnostics snapshot (see module docstring)."""
    clean = {}
    if snapshot.get("status") in STATUSES:
        clean["status"] = snapshot["status"]
    if _identifier(snapshot.get("phase")):
        clean["phase"] = snapshot["phase"]
    clean.update({k: snapshot[k] for k in METRICS if _number(snapshot.get(k))})
    if isinstance(snapshot.get("plan"), dict):
        clean["plan"] = sanitize_plan(snapshot["plan"])
    metrics = snapshot.get("aggregate_metrics")
    if isinstance(metrics, dict):
        aggregate = {k: metrics[k] for k in AGGREGATE_METRICS if _number(metrics.get(k))}
        if _identifier(metrics.get("strategy")):
            aggregate["strategy"] = metrics["strategy"]
        if aggregate:
            clean["aggregate_metrics"] = aggregate
    return clean


def utc_now():
    return datetime.datetime.now(datetime.timezone.utc)


def _count(value):
    return value if type(value) is int else None


def tiers(client):
    """Keys, endpoint used_memory and, when reported, the RAM/flash object split.

    The shard reports the split as ram= and disk= on bigdb0 in its
    Bigredis-Keyspace section. The proxy answers one section per INFO call."""
    keyspace = client.info("keyspace") or {}
    big = (client.info("Bigredis-Keyspace") or {}).get("bigdb0") or {}
    memory = client.info("memory") or {}
    db0 = keyspace.get("db0") or {}
    ram, flash = _count(big.get("ram")), _count(big.get("disk"))
    # INFO omits db0 when the database has no keys.
    return {"keys": _count(db0.get("keys")) if db0 else 0,
            "endpoint_used_memory": _count(memory.get("used_memory")),
            "ram_objects": ram, "flash_objects": flash,
            "split_available": ram is not None and flash is not None}


def flex_tiers(private):
    """Sample tiers() over RESP3 with the profile's URI; failures are recorded, never raised."""
    import flex_compaction

    state = {"captured_utc": utc_now().isoformat()}
    client = None
    try:
        config = json.loads((Path(private) / "connection.json").read_text())
        client = flex_compaction.redis_client(config)
        state.update(tiers(client))
    except Exception as exc:  # the type only: messages can carry the URI
        state = {"captured_utc": state["captured_utc"], "error": type(exc).__name__}
    finally:
        if client is not None:
            try:
                client.close()
            except Exception:
                pass
    return state
