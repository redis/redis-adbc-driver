"""Redis ADBC driver. Native release wheels bundle the shared library."""

import enum
import os
from pathlib import Path

import adbc_driver_manager


def _driver_path():
    override = os.environ.get("REDIS_ADBC_LIBRARY")
    if override:
        path = Path(override).expanduser().resolve(strict=True)
        if not path.is_file():
            raise FileNotFoundError(path)
        return str(path)
    for name in (
        "libadbc_driver_redis.so",
        "libadbc_driver_redis.dylib",
        "adbc_driver_redis.dll",
    ):
        path = Path(__file__).resolve().parent / name
        if path.is_file():
            return str(path)
    raise FileNotFoundError(
        "Redis driver library is missing. Install a platform wheel or set REDIS_ADBC_LIBRARY to a built library."
    )


def connect(uri, db_kwargs=None):
    """Create a low-level ADBC database; the caller owns its lifetime."""
    return adbc_driver_manager.AdbcDatabase(
        driver=_driver_path(), **{**(db_kwargs or {}), "uri": uri}
    )


class DatabaseOptions(str, enum.Enum):
    TELEMETRY_ENABLED = "adbc.redis.telemetry.enabled"
    TELEMETRY_ENDPOINT = "adbc.redis.telemetry.endpoint"
    TELEMETRY_TOKEN = "adbc.redis.telemetry.token"
    TELEMETRY_INSTALLATION_ID = "adbc.redis.telemetry.installation_id"
    TELEMETRY_ORGANIZATION = "adbc.redis.telemetry.organization"
    TELEMETRY_TEAM = "adbc.redis.telemetry.team"
    TELEMETRY_APPLICATION = "adbc.redis.telemetry.application"
    TELEMETRY_ENVIRONMENT = "adbc.redis.telemetry.environment"
    TELEMETRY_USE_CASE = "adbc.redis.telemetry.use_case"
    TELEMETRY_INTEGRATION = "adbc.redis.telemetry.integration"
    TELEMETRY_INTERVAL = "adbc.redis.telemetry.interval"
    TELEMETRY_SNAPSHOT = "adbc.redis.telemetry.snapshot"
    TELEMETRY_FLUSH = "adbc.redis.telemetry.flush"
    DEFAULT_SCHEMA = "adbc.redis.default_schema"
    ADDRESS = "adbc.redis.address"
    DB = "adbc.redis.db"
    CLUSTER = "adbc.redis.cluster"
    READ_TIMEOUT = "adbc.redis.read_timeout"
    WRITE_TIMEOUT = "adbc.redis.write_timeout"
    TIME_ZONE = "adbc.redis.time_zone"
    AGGREGATE_PUSHDOWN = "adbc.redis.aggregate_pushdown"
    QUERY_WORKERS = "adbc.redis.query_workers"
    QUERY_WORKER_BUDGET = "adbc.redis.query_worker_budget"
    RENAME_REKEY = "adbc.redis.rename_rekey"
    FOREIGN_INDEXES = "adbc.redis.foreign_indexes"
    STREAM_RESULTS = "adbc.redis.stream_results"
    STREAM_BATCH_ROWS = "adbc.redis.stream_batch_rows"
    PARTITION_RESULT_NAMESPACE = "adbc.redis.partition_result_namespace"
    PARTITION_READER_ONLY = "adbc.redis.partition_reader_only"
    PARTITION_RESULT_LIMITS = "adbc.redis.partition_result_limits"
    PARTITION_CONTROL_PERSISTENCE = "adbc.redis.partition_control_persistence"


class StatementOptions(str, enum.Enum):
    IPC_INPUT = "adbc.redis.ipc.input"
    IPC_OUTPUT = "adbc.redis.ipc.output"
    IPC_FORMAT = "adbc.redis.ipc.format"
    PARTITION_COUNT = "adbc.redis.partition_count"
    PARTITION_RESULT_MODE = "adbc.redis.partition_result_mode"
    PARTITION_RESULT_TTL = "adbc.redis.partition_result_ttl"
    PARTITION_EXECUTION_LIMITS = "adbc.redis.partition_execution_limits"
    INGEST_INDEX_COLUMNS = "adbc.redis.ingest.index_columns"
    AGGREGATE_PUSHDOWN = "adbc.redis.aggregate_pushdown"
    STREAM_RESULTS = "adbc.redis.stream_results"
    STREAM_BATCH_ROWS = "adbc.redis.stream_batch_rows"
    EXECUTION_DIAGNOSTICS_FILE = "adbc.redis.execution_diagnostics_file"
