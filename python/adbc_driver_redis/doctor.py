"""Installation diagnostics. Never echo a URI or raw native-driver exception."""

import ctypes
import importlib.metadata
import platform
import sys

from . import _driver_path


def connection_hint(error):
    # Errors may include credentials, URI query values or server-provided text.
    # Classify privately and emit only our own fixed messages.
    message = str(error).lower()
    if any(word in message for word in ("wrongpass", "noauth", "authentication")):
        return "Authentication failed. Check the Redis ACL username and password in REDIS_URI."
    if "noperm" in message or "permission" in message:
        return "Redis denied a command. Check the ACL permissions in the server requirements guide."
    if any(word in message for word in ("certificate", "tls", "x509")):
        return "TLS failed. Use rediss:// with the correct hostname and trusted CA certificates."
    if "flex" in message:
        return "Redis Flex is unsupported. Use a supported RAM database with Search."
    if "search" in message or "ft._list" in message:
        return "Search is unavailable. Enable the Query Engine and allow FT._LIST in the Redis ACL."
    if any(word in message for word in ("timeout", "timed out", "deadline")):
        return "Connection timed out. Check the endpoint, port, network access and server availability."
    return "Connection/query failed. Check REDIS_URI, network access, server requirements and ACL permissions."


def _query(uri, driver=None):
    from . import dbapi

    options = {"adbc.redis.read_timeout": "5s", "adbc.redis.write_timeout": "5s"}
    if driver is None:
        connection = dbapi.connect(uri, db_kwargs=options)
    else:
        import adbc_driver_manager.dbapi as manager

        connection = manager.connect(
            driver=driver, db_kwargs={**options, "uri": uri}, autocommit=True
        )
    with connection as conn, conn.cursor() as cursor:
        cursor.execute("SELECT 1 AS one")
        if cursor.fetchall() != [(1,)]:
            raise ValueError("Unexpected SELECT 1 result")


def diagnose(*, uri=None, check_manifests=False, output=None):
    output = output or sys.stdout

    def report(status, message):
        print(f"{status}: {message}", file=output)

    system, machine = sys.platform, platform.machine().lower()
    if (system == "linux" and machine in ("x86_64", "amd64", "aarch64", "arm64")) or (
        system == "darwin" and machine == "arm64"
    ):
        report("INFO", "Wheel target recognized; native loading below checks runtime compatibility.")
    else:
        report("WARN", "No published wheel for this platform. Use a supported platform or a source-built REDIS_ADBC_LIBRARY.")
    for name in ("adbc-driver-redis", "adbc-driver-manager"):
        try:
            report("OK", f"{name} {importlib.metadata.version(name)} installed.")
        except importlib.metadata.PackageNotFoundError:
            report("WARN", f"{name} package metadata missing (possible source checkout).")
    try:
        library = ctypes.CDLL(_driver_path())
        library.AdbcDriverInit
    except Exception:
        report("FAIL", "Native library missing or unloadable. Check or unset REDIS_ADBC_LIBRARY, then reinstall a matching platform wheel: python -m pip install --force-reinstall 'adbc-driver-redis[dbapi]'; for source builds set REDIS_ADBC_LIBRARY to a valid library.")
        return 1
    report("OK", "Native library loads and exports AdbcDriverInit.")
    try:
        from . import dbapi  # noqa: F401
    except ImportError:
        report("FAIL", "DB-API dependencies missing. Run: python -m pip install 'adbc-driver-redis[dbapi]'")
        return 1
    report("OK", "DB-API dependencies available.")
    if uri is None:
        report("SKIP", "Connection and manifest discovery not tested. Set REDIS_URI and rerun doctor --check-manifests.")
        return 0
    try:
        _query(uri)
    except Exception as error:
        report("FAIL", connection_hint(error))
        return 1
    report("OK", "Connected; driver Search checks and SELECT 1 passed. TLS verification follows the supplied URI.")
    if check_manifests:
        failed = False
        for driver in ("redis", "rediss"):
            try:
                _query(uri, driver)
            except Exception:
                report("FAIL", f"{driver} discovery/query failed. Run: python -m adbc_driver_redis install-manifest --user; use --venv for environment-scoped clients, or set ADBC_DRIVER_PATH for a custom manifest directory. Retry after correcting discovery or connection settings.")
                failed = True
            else:
                report("OK", f"driver='{driver}' discovery and SELECT 1 passed.")
        return int(failed)
    report("SKIP", "Manifest discovery not tested. Add --check-manifests if your tool loads driver='redis'.")
    return 0
