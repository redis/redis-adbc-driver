# Intent

Date: 2026-11-05. Source: 95d6583. Requestor: repository user, via
[issue #322](https://github.com/redis/redis-adbc-driver/issues/322).
Status: experimental implementation; review not yet performed by a maintainer.

Enable SQL reads/writes on newer Software Flex while SORTABLE and aggregate
cursors remain unavailable. Deliver an explicit, restricted stopgap while keeping
standard execution unchanged. Cloud and older Flex releases require separate
validation. See [spec](spec.md) and [plan](plan.md).
