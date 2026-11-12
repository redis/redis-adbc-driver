# Unmanaged HASH tables

Date: 2026-11-12. Base: a8de1c3c. Requestor: Albert Wong. Status: implemented locally.

Allow HASH adoption and foreign-index registration to expose application-owned
data without copying it. The user explicitly requested SQL writes to source
HASHes as well as live reads. Managed copies remain the default.

See [specification](spec.md), [plan](plan.md), and
[pull request #394](https://github.com/redis/redis-adbc-driver/pull/394).
