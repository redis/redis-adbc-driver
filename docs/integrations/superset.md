# Apache Superset integration status

Superset 6.1 is unsupported under this project's Python 3.14-or-newer policy.
It requires PyArrow below 19, which has no usable CPython 3.14 wheels on the
checked Linux platform. The older Python environment, integration extra and
supported CI/profile entry have been removed.

[Issue #471](https://github.com/redis/redis-adbc-driver/issues/471) tracks upstream dependency support and restoration.
Upstream discussion: [Superset SIP-234](https://github.com/apache/superset/issues/45019).
The proposal's closed status does not prove a compatible released dependency set.

A future supported Superset release must resolve/install on Python 3.14+ without
dependency overrides and pass the real application, dataset/chart and SQL Lab
checks against verified Redis Software. Historical test records retain their
actual runtimes; they do not establish current support.
