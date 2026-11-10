"""Independent PostgreSQL binary record decoding and Arrow composite values.

The wire record carries every field's OID, including NULL fields. This avoids
inferring PostgreSQL types or values from the driver's Arrow output.
"""
from dataclasses import dataclass
import struct

from psycopg import pq
from psycopg.adapt import Loader, Transformer


@dataclass
class Composite:
    oids: list[int] | None
    types: list[str] | None
    fields: list


class CompositeLoader(Loader):
    format = pq.Format.BINARY

    def __init__(self, oid, context=None):
        super().__init__(oid, context)
        self.context = context

    def load(self, data):
        count = struct.unpack_from("!i", data)[0]
        if count < 0:
            raise ValueError("negative record field count")
        pos, oids, values = 4, [], []
        for _ in range(count):
            oid, size = struct.unpack_from("!Ii", data, pos)
            pos += 8
            if size < -1 or pos + max(0, size) > len(data):
                raise ValueError("invalid record field length")
            oids.append(oid)
            values.append(None if size == -1 else data[pos:pos + size])
            pos += max(0, size)
        if pos != len(data):
            raise ValueError("trailing record data")
        tx = Transformer(self.context)
        tx.set_loader_types(oids, pq.Format.BINARY)
        return Composite(oids, None, list(tx.load_sequence(values)))


@dataclass(frozen=True)
class CatalogName:
    oid: int
    kind: int


class CatalogNameLoader(Loader):
    format = pq.Format.BINARY

    def load(self, data):
        return CatalogName(struct.unpack("!I", data)[0], self.oid)


def resolve_catalog_names(conn, v, cache):
    # regclass/regtype binary values carry OIDs rather than their SQL display
    # names. Resolve through the reference's own catalog and search_path,
    # after fetching the original result; never use Redis names as the oracle.
    if isinstance(v, CatalogName):
        if v not in cache:
            cast = {2205: "regclass", 2206: "regtype"}[v.kind]
            with conn.cursor() as cur:
                cur.execute(f"SELECT %s::oid::{cast}::text", (v.oid,))
                cache[v] = cur.fetchone()[0]
        return cache[v]
    if isinstance(v, Composite):
        return Composite(v.oids, v.types, [resolve_catalog_names(conn, x, cache) for x in v.fields])
    if isinstance(v, (list, tuple)):
        return [resolve_catalog_names(conn, x, cache) for x in v]
    return v


class DateLoader(Loader):
    format = pq.Format.BINARY

    def load(self, data):
        from run import civil_date
        n = struct.unpack("!i", data)[0]
        if n in (-2147483648, 2147483647):
            return "-infinity" if n < 0 else "infinity"
        text, bc = civil_date(n + 10957)
        return text + (" BC" if bc else "")


class TimestampLoader(Loader):
    format = pq.Format.BINARY

    def load(self, data):
        from run import civil_date
        n = struct.unpack("!q", data)[0]
        if n in (-9223372036854775808, 9223372036854775807):
            return "-infinity" if n < 0 else "infinity"
        seconds, fraction = divmod(n, 1000000)
        days, clock = divmod(seconds, 86400)
        text, bc = civil_date(days + 10957)
        hour, clock = divmod(clock, 3600)
        minute, second = divmod(clock, 60)
        text += f" {hour:02}:{minute:02}:{second:02}"
        if fraction:
            text += "." + f"{fraction:06}".rstrip("0")
        if self.oid == 1184:
            text += "+00"
        return text + (" BC" if bc else "")


class IntervalLoader(Loader):
    format = pq.Format.BINARY

    def load(self, data):
        microseconds, days, months = struct.unpack("!qii", data)
        # The existing comparator accepts month/day/nanosecond objects.
        from types import SimpleNamespace
        return SimpleNamespace(months=months, days=days, nanoseconds=microseconds * 1000)


class TimeTZLoader(Loader):
    format = pq.Format.BINARY

    def load(self, data):
        micros, west = struct.unpack("!qi", data)
        seconds, fraction = divmod(micros, 1000000)
        hour, seconds = divmod(seconds, 3600)
        minute, second = divmod(seconds, 60)
        text = f"{hour:02}:{minute:02}:{second:02}"
        if fraction:
            text += "." + f"{fraction:06}".rstrip("0")
        east = -west
        hours, remainder = divmod(abs(east), 3600)
        minutes, seconds = divmod(remainder, 60)
        text += ("-" if east < 0 else "+") + f"{hours:02}"
        if minutes or seconds:
            text += f":{minutes:02}"
        if seconds:
            text += f":{seconds:02}"
        return text


class StringLoader(Loader):
    format = pq.Format.BINARY

    def load(self, data):
        return bytes(data[1:] if self.oid == 3802 else data).decode("utf-8")


def register(conn):
    for oid, loader in ((2249, CompositeLoader), (2205, CatalogNameLoader), (2206, CatalogNameLoader), (1082, DateLoader),
                        (1114, TimestampLoader), (1184, TimestampLoader),
                        (1186, IntervalLoader), (1266, TimeTZLoader), (114, StringLoader), (3802, StringLoader)):
        conn.adapters.register_loader(oid, loader)
