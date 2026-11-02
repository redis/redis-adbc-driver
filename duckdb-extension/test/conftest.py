import os

import pytest


def pytest_sessionstart(session):
    from urllib.parse import parse_qs, urlsplit

    for name in ("REDIS_URI", "REDIS_ADMIN_URI", "REDIS_FLEX_URI"):
        value = os.environ.get(name)
        if not value:
            continue
        try:
            uri = urlsplit(value)
            valid = (uri.scheme == "rediss" and uri.hostname and uri.username and uri.password
                     and not {"skip_verify", "ssl_cert_reqs", "ssl_check_hostname"}.intersection(parse_qs(uri.query)))
        except ValueError:
            valid = False
        if not valid:
            raise pytest.UsageError(
                f"{name} must use rediss:// with a username and password on Redis Enterprise; trust its TLS CA"
            )
