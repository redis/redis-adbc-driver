# Intent

Draft implementation: [PR607](https://github.com/redis/redis-adbc-driver/pull/607).

Date: 2026-11-23. Origin: follow-up to [PR606](https://github.com/redis/redis-adbc-driver/pull/606). Status: local candidate, no maintainer approval inferred.
Requested through the TPC-DS issue investigation; related to
[TPC-DS Q4 issue 590](https://github.com/redis/redis-adbc-driver/issues/590).
See [specification](spec.md), [plan](plan.md), and [verification](verification.md).

Q4 still exhausts the focused worker's memory because grouped fact/customer/date
branches materialize fact operands and full join intermediates before reduction.
Deferring unused CTE alias copies does not address those allocations. Stream a
conservative, generic inner star join directly into existing exact aggregation.
Keep full benchmark suites stopped and leave unsupported plans on their existing
execution path. This candidate has no live performance claim.
