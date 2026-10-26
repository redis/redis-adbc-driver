"""ADBC DB-API facade, defaulting to the driver's only supported commit mode."""

import adbc_driver_manager.dbapi as _manager
from adbc_driver_manager.dbapi import *

from . import _driver_path

paramstyle = "qmark"


def connect(uri, db_kwargs=None, conn_kwargs=None, *, autocommit=True, **kwargs):
    if not autocommit:
        raise _manager.NotSupportedError(
            "Redis ADBC supports autocommit=True only; transactions are not implemented"
        )
    return _manager.connect(
        driver=_driver_path(),
        db_kwargs={**(db_kwargs or {}), "uri": uri},
        conn_kwargs=conn_kwargs,
        autocommit=True,
        **kwargs,
    )
