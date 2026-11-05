"""Create a new local disposable Redis benchmark table; never replace a table."""

import argparse
from collections import Counter
import json
import os
from urllib.parse import urlsplit

import adbc_driver_manager.dbapi as dbapi

from run import fixture, queries


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--driver", required=True)
    parser.add_argument("--table", default="gpu_fixture")
    parser.add_argument("--rows", type=int, default=1_000_000)
    parser.add_argument("--groups", type=int, default=1000)
    args = parser.parse_args()
    if args.rows <= 0 or args.groups <= 0:
        parser.error("rows and groups must be positive")
    uri = os.environ.get("REDIS_URI", "")
    endpoint = urlsplit(uri)
    if (endpoint.scheme != "redis" or endpoint.hostname not in ("localhost", "127.0.0.1")
            or endpoint.path not in ("", "/", "/0")):
        parser.error("REDIS_URI must name a disposable local Redis Software database /0")
    table = fixture(args.rows, args.groups)
    with dbapi.connect(driver=os.path.abspath(args.driver), autocommit=True,
                       db_kwargs={"uri": uri}) as conn, conn.cursor() as cur:
        cur.adbc_ingest(args.table, table, mode="create")
        cur.execute(queries("public", args.table)[0])
        actual = cur.fetch_arrow_table()
        if actual.schema != table.schema:
            raise ValueError("fixture Arrow schema mismatch")
        # ADBC scan order is unspecified; multiplicities and NULLs must survive.
        expected_rows = Counter(zip(table["bucket"].to_pylist(), table["value"].to_pylist()))
        actual_rows = Counter(zip(actual["bucket"].to_pylist(), actual["value"].to_pylist()))
        if expected_rows != actual_rows:
            raise ValueError("fixture values/multiplicities differ after full fetch")
    print(json.dumps({"rows": args.rows, "groups_parameter": args.groups,
                      "full_fetch_values_validated": True}))


if __name__ == "__main__":
    main()
