"""Bootstrap the upstream external-dialect compliance plugin."""

import pytest
import os
from sqlalchemy.dialects import registry
from sqlalchemy.testing import provision

# The compliance plugin compares explicit-driver URLs with the backend's
# default driver. These aliases are scoped to this test process; application
# URLs continue to use redis+adbc/rediss+adbc entry points.
for backend in ("redis", "rediss"):
    registry.register(backend, "adbc_driver_redis.sqlalchemy", "RedisDialect")


@provision.temp_table_keyword_args.for_db("redis", "rediss")
def redis_temp_table_keyword_args(cfg, eng):
    return {"prefixes": ["TEMPORARY"]}


@provision.update_db_opts.for_db("redis", "rediss")
def redis_db_options(db_url, db_opts, options):
    if os.environ.get("REDIS_ACCEPTANCE_SCHEMA"):
        db_opts["connect_args"] = {"db_kwargs": {"adbc.redis.default_schema": os.environ["REDIS_ACCEPTANCE_SCHEMA"]}}

pytest.register_assert_rewrite("sqlalchemy.testing.assertions")

from sqlalchemy.testing.plugin.pytestplugin import *  # noqa: F401,F403,E402
from sqlalchemy.testing.plugin.pytestplugin import pytest_collection_modifyitems as _sqlalchemy_collection


def pytest_collection_modifyitems(session, config, items):
    _sqlalchemy_collection(session, config, items)
    for item in items:
        if item.name.startswith("test_round_trip_same_named_column[___name]"):
            item.add_marker(pytest.mark.skip(reason="Column names beginning with __ are reserved for Redis metadata"))
        elif item.name.startswith("test_long_convention_name[ix-"):
            item.add_marker(pytest.mark.skip(reason="Redis Search table indexes do not implement CREATE INDEX"))
