# Copyright (c) 2026 ADBC Drivers Contributors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#         http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""TIME WITH TIME ZONE through the C API: Arrow has no such type, so a
column of one is a string column of its text (as Postgres writes it), whose
field metadata ARROW:FLIGHT:SQL:TYPE_NAME names the type."""

import adbc_driver_manager.dbapi
import pyarrow
import pytest

TABLE = "it_timetz_c"
NEW_TABLE = "it_timetz_c_new"
TYPE_NAME = b"ARROW:FLIGHT:SQL:TYPE_NAME"


@pytest.fixture
def tz_conn(driver_path, db_kwargs):
    kwargs = {**db_kwargs, "adbc.redis.time_zone": "Asia/Kolkata"}
    with adbc_driver_manager.dbapi.connect(driver=driver_path, db_kwargs=kwargs, autocommit=True) as conn:
        with conn.cursor() as cur:
            for t in [TABLE, NEW_TABLE]:
                cur.execute(f"DROP TABLE IF EXISTS {t}")
            cur.execute(f"CREATE TABLE {TABLE} (id INTEGER, t TIMETZ, t3 TIME(3) WITH TIME ZONE)")
            cur.execute(
                f"INSERT INTO {TABLE} VALUES (1, '10:00+02', '10:00:00.1234+02'), (2, '09:00+01', '23:00-05'),"
                " (3, '08:00+00', '10:00'), (4, NULL, NULL)"
            )
        yield conn
        with conn.cursor() as cur:
            for t in [TABLE, NEW_TABLE]:
                cur.execute(f"DROP TABLE IF EXISTS {t}")


def test_arrow_type(tz_conn) -> None:
    with tz_conn.cursor() as cur:
        cur.execute(f"SELECT t, t3, CURRENT_TIME AS c, LOCALTIME AS l FROM {TABLE} WHERE id = 1")
        table = cur.fetch_arrow_table()
    schema = table.schema
    for name, type_name in [("t", b"TIME(6) WITH TIME ZONE"), ("t3", b"TIME(3) WITH TIME ZONE"), ("c", b"TIME(6) WITH TIME ZONE")]:
        assert schema.field(name).type == pyarrow.string(), name
        assert schema.field(name).metadata == {TYPE_NAME: type_name}, name
    assert schema.field("l").type == pyarrow.time64("us")
    assert table.column("t").to_pylist() == ["10:00:00+02"]
    assert table.column("t3").to_pylist() == ["10:00:00.123+02"]
    # CURRENT_TIME has the session time zone's offset.
    assert table.column("c").to_pylist()[0].endswith("+05:30")

    schema = tz_conn.adbc_get_table_schema(TABLE)
    assert schema.field("t").type == pyarrow.string()
    assert schema.field("t").metadata == {TYPE_NAME: b"TIME(6) WITH TIME ZONE"}


def test_values(tz_conn) -> None:
    """Postgres's order: by the time in UTC, then larger offsets first; a
    time without an offset has the session time zone's."""
    with tz_conn.cursor() as cur:
        cur.execute(f"SELECT id, t, t3 FROM {TABLE} ORDER BY t, id")
        assert cur.fetchall() == [
            (1, "10:00:00+02", "10:00:00.123+02"),
            (2, "09:00:00+01", "23:00:00-05"),
            (3, "08:00:00+00", "10:00:00+05:30"),
            (4, None, None),
        ]
        cur.execute(f"SELECT id FROM {TABLE} WHERE t = '10:00+02' OR t3 = '10:00'")
        assert sorted(r[0] for r in cur.fetchall()) == [1, 3]
        cur.execute(f"SELECT t AT TIME ZONE 'UTC', CAST(t AS TIME), TIME '10:00' AT TIME ZONE 'UTC' FROM {TABLE} WHERE id = 1")
        row = cur.fetchone()
        assert row[0] == "08:00:00+00"
        assert str(row[1]) == "10:00:00"
        assert row[2] == "04:30:00+00"
        # A bound string is read as a time with time zone.
        cur.execute(f"SELECT id FROM {TABLE} WHERE t = $1", parameters=("09:00+01",))
        assert cur.fetchall() == [(2,)]


def test_ingest(tz_conn) -> None:
    """Text is read as a time with time zone, and a string field whose
    metadata names the type makes such a column."""
    with tz_conn.cursor() as cur:
        cur.adbc_ingest(TABLE, pyarrow.table({"id": pyarrow.array([5, 6], pyarrow.int32()), "t": ["11:00+03", "11:00"]}), mode="append")
        cur.execute(f"SELECT id, t FROM {TABLE} WHERE id >= 5 ORDER BY id")
        assert cur.fetchall() == [(5, "11:00:00+03"), (6, "11:00:00+05:30")]

        field = pyarrow.field("t", pyarrow.string(), metadata={TYPE_NAME: b"TIME(3) WITH TIME ZONE"})
        data = pyarrow.table({"t": pyarrow.array(["10:00:00.5-05:30"])}, schema=pyarrow.schema([field]))
        cur.adbc_ingest(NEW_TABLE, data, mode="create")
        cur.execute(f"SELECT data_type FROM information_schema.columns WHERE table_name = '{NEW_TABLE}'")
        assert cur.fetchall() == [("TIME(3) WITH TIME ZONE",)]
        cur.execute(f"SELECT t FROM {NEW_TABLE}")
        assert cur.fetchall() == [("10:00:00.5-05:30",)]
