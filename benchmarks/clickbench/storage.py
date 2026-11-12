"""Explicit ClickBench storage intent; this does not certify server support."""

FULL_ROWS = 99_997_497


def storage_mode(rows, requested=None):
    mode = requested or ("flex-stopgap" if rows == FULL_ROWS else "ram")
    if mode not in ("ram", "flex-stopgap"):
        raise ValueError("Storage must be ram or flex-stopgap")
    return mode


def driver_options(mode):
    if mode not in ("ram", "flex-stopgap"):
        raise ValueError("Storage must be ram or flex-stopgap")
    return {"adbc.redis.flex": "true" if mode == "flex-stopgap" else "false"}
