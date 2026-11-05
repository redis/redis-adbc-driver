"""Probe an explicitly designated disposable Cloud Flex endpoint without logging secrets."""
import json
import os
import sys
from pathlib import Path

import adbc_driver_manager
import adbc_driver_manager.dbapi
import redis


def main():
    uri = os.environ["REDIS_FLEX_URI"]
    ca = os.environ["SSL_CERT_FILE"]
    raw = redis.Redis.from_url(uri, protocol=3, ssl_ca_certs=ca, decode_responses=True)
    result = {"transport": "authenticated TLS", "protocol": "RESP3"}
    try:
        assert raw.ping()
        result["redis_version"] = raw.info("server").get("redis_version")
        before = raw.dbsize()
        try:
            raw.execute_command("FT._LIST")
            result["search_list"] = "accepted"
        except redis.ResponseError:
            result["search_list"] = "rejected"
        ext = "dylib" if sys.platform == "darwin" else "so"
        driver = os.environ.get("REDIS_ADBC_LIBRARY", str(Path(__file__).resolve().parents[1] / "build" / f"libadbc_driver_redis.{ext}"))
        try:
            conn = adbc_driver_manager.dbapi.connect(
                driver=driver,
                db_kwargs={"uri": uri, "adbc.redis.flex": "true", "adbc.redis.tls_ca_file": ca, "adbc.redis.telemetry.enabled": "false"},
                autocommit=True,
            )
        except adbc_driver_manager.NotSupportedError:
            result["driver_open"] = "rejected_not_implemented"
            result["key_count_unchanged"] = raw.dbsize() == before
            assert result["key_count_unchanged"]
        else:
            result["driver_open"] = "accepted"
            try:
                with conn.cursor() as cur:
                    cur.execute("CREATE TABLE it_cloud_flex_probe (n BIGINT, s VARCHAR)")
                    try:
                        cur.execute("INSERT INTO it_cloud_flex_probe VALUES (10,'ten'),(2,'two'),(NULL,NULL)")
                        cur.execute("SELECT n,s FROM it_cloud_flex_probe WHERE n>=2 ORDER BY n")
                        assert cur.fetchall() == [(2,"two"),(10,"ten")]
                        cur.execute("SELECT count(*),count(n),sum(n) FROM it_cloud_flex_probe")
                        assert cur.fetchall() == [(3,2,12)]
                        result["sql_rows_fully_validated"] = True
                    finally:
                        cur.execute("DROP TABLE it_cloud_flex_probe")
            finally:
                conn.close()
        print(json.dumps(result, sort_keys=True))
    finally:
        raw.close()


if __name__ == "__main__":
    main()
