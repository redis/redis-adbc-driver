"""Bootstrap the upstream external-dialect compliance plugin."""

import pytest
from sqlalchemy.dialects import registry

# The compliance plugin compares explicit-driver URLs with the backend's
# default driver. These aliases are scoped to this test process; application
# URLs continue to use redis+adbc/rediss+adbc entry points.
for backend in ("redis", "rediss"):
    registry.register(backend, "adbc_driver_redis.sqlalchemy", "RedisDialect")

pytest.register_assert_rewrite("sqlalchemy.testing.assertions")

from sqlalchemy.testing.plugin.pytestplugin import *  # noqa: F401,F403,E402
