# Intent

Date: 2026-11-24. Status: focused lifecycle correction.
Origin: review of bounded parallel fact streaming from
[PR612](https://github.com/redis/redis-adbc-driver/pull/612).
See [specification](spec.md), [plan](plan.md), [verification](verification.md).

A coordinator visitor panic can bypass the parallel core's normal worker wait.
The outer deferred worker-budget release may then return tokens while canceled
workers still own source iterators. Join those workers before propagating the
same panic and releasing the grant. Keep this fix separate from optional
representative-map ownership or allocation optimizations.
