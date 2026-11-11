"""Redis-style managed HASH reads using the shared native reader, without SQL.

This initial API binds an exact table and uses positive internal __rowid values.
It is not a drop-in redis-py client. Values are stored Redis bytes; SQL NULLs
and unknown fields are absent. Native storage encodings are not type-converted.
"""

import base64
from contextlib import contextmanager
import ctypes
import json
import threading

from . import _driver_path


class NativeReadError(RuntimeError):
    """A native managed-table read or snapshot operation failed."""


class NativeClient:
    """Own a serialized native reader. Close explicitly or use a context manager.

    Each ordinary call sees its own committed snapshot. snapshot() creates a
    dedicated reader whose table reads share one fixed view. Backend connection
    options, TLS, RESP3 and authorization are the driver's existing policies.
    """

    def __init__(self, uri, *, db_kwargs=None, decode_responses=False, encoding="utf-8"):
        self._lock = threading.RLock()
        self._handle = None
        self._options = {**(db_kwargs or {}), "uri": uri}
        if not all(isinstance(k, str) and isinstance(v, str) for k, v in self._options.items()):
            raise TypeError("database option names and values must be strings")
        self._decode = decode_responses
        self._encoding = encoding
        self._library_path = _driver_path()
        self._lib = ctypes.CDLL(self._library_path)
        try:
            self._call = self._lib.RedisNativeReadV1
            self._free = self._lib.RedisNativeReadFreeV1
        except AttributeError as exc:
            raise NativeReadError("loaded Redis library does not support native reads") from exc
        self._call.argtypes = [ctypes.c_char_p]
        self._call.restype = ctypes.c_void_p
        self._free.argtypes = [ctypes.c_void_p]
        self._free.restype = None
        self._handle = self._request("open", options=self._options)

    def _request(self, action, **kwargs):
        with self._lock:
            if action != "open" and self._handle is None:
                raise NativeReadError("native client is closed")
            payload = json.dumps({"action": action, "handle": self._handle or 0, **kwargs})
            ptr = self._call(payload.encode("utf-8"))
            if not ptr:
                raise NativeReadError("native reader returned no reply")
            try:
                reply = json.loads(ctypes.string_at(ptr))
            finally:
                self._free(ptr)
            if reply.get("error"):
                raise NativeReadError(reply["error"])
            return reply["result"]

    def close(self):
        with self._lock:
            if self._handle is not None:
                try:
                    self._request("close")
                finally:
                    self._handle = None

    def __enter__(self):
        if self._handle is None:
            raise NativeReadError("native client is closed")
        return self

    def __exit__(self, *_):
        self.close()

    def table(self, name, *, schema="public"):
        """Bind exact identifiers; row arguments will be internal __rowid values."""
        if not isinstance(name, str) or not name or not isinstance(schema, str) or not schema:
            raise ValueError("schema and table must be nonempty strings")
        return NativeTable(self, schema, name)

    @contextmanager
    def snapshot(self, *, retention_ms=900000):
        """Yield a dedicated client bound to one view; close it on every exit.

        Lifetime is fixed, 120000..3600000 ms. Errors never switch to a newer
        view. Ordinary reads on the parent client continue using fresh views.
        """
        if type(retention_ms) is not int or not 120000 <= retention_ms <= 3600000:
            raise ValueError("retention_ms must be 120000..3600000")
        with self._lock:
            if self._handle is None:
                raise NativeReadError("native client is closed")
            view = NativeClient(self._options["uri"], db_kwargs=self._options,
                                decode_responses=self._decode, encoding=self._encoding)
        try:
            view._request("acquire", retention_ms=retention_ms)
            yield view
        finally:
            view.close()


class NativeTable:
    """HASH-style methods scoped to a managed table, addressed by internal row ID."""

    def __init__(self, client, schema, name):
        self._client, self.schema, self.name = client, schema, name

    def _read(self, row_id, fields):
        if type(row_id) is not int or not 0 < row_id <= 2**63 - 1:
            raise ValueError("row_id must be a positive internal int64 __rowid")
        if fields is not None and not all(isinstance(f, str) for f in fields):
            raise TypeError("field names must be strings")
        reply = self._client._request("read", schema=self.schema, table=self.name,
                                      row_id=row_id, fields=fields)
        row = {k: base64.b64decode(v, validate=True) for k, v in reply["row"].items()}
        return row, reply["exists"]

    def _value(self, value):
        if value is not None and self._client._decode:
            return value.decode(self._client._encoding)
        return value

    def hgetall(self, row_id):
        row, _ = self._read(row_id, None)
        return {(k if self._client._decode else k.encode("utf-8")): self._value(v)
                for k, v in row.items()}

    def hmget(self, row_id, fields):
        if isinstance(fields, (str, bytes)):
            raise TypeError("fields must be an iterable of field names")
        fields = list(fields)
        if not fields:
            raise ValueError("hmget requires at least one field")
        row, _ = self._read(row_id, fields)
        return [self._value(row.get(f)) for f in fields]

    def hget(self, row_id, field):
        return self.hmget(row_id, [field])[0]

    def hexists(self, row_id, field):
        row, _ = self._read(row_id, [field])
        return field in row

    def hlen(self, row_id):
        row, _ = self._read(row_id, None)
        return len(row)

    def exists(self, row_id):
        _, exists = self._read(row_id, [])
        return exists
