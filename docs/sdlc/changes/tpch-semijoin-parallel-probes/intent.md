# Intent

Requested 2026-11-24 by the TPC-H issues session for the user: run semi-join probe
batches on bounded concurrent workers, as a follow-up to
[#632](https://github.com/redis/redis-adbc-driver/pull/632) for
[#587](https://github.com/redis/redis-adbc-driver/issues/587) (Q21) and
[#580](https://github.com/redis/redis-adbc-driver/issues/580) (Q4). Base:
`1245ac2cdd944c68324f48dac7084b9b8c49b271` (main, with #630 and #632 merged).
Status: local change, SF1 verification pending. See [spec](spec.md),
[plan](plan.md), [validation](validation.md).

After #630 and #632, decorrelated EXISTS / IN subqueries read a page's inner
groups in 256-key union probes, but one at a time. On the 10% subset, Q21's 58
probes took about 6.9 of 8 s. Each probe mostly waits on its own round trips
(five keyset Search pages and HASH fetch pipelines, about 113 ms, against about
11 ms of server union time). SF1 has about ten times the outer keys, so serial
probes alone would take about 70 s, over the 60 s deadline.

Desired: a page's probe batches run on workers granted by the database's
existing worker budget, with unchanged results, `probeFailed` / per-row
fallbacks, errors, cancellation and panics. Excluded: a different batch size
(256 stays; [#631](https://github.com/redis/redis-adbc-driver/issues/631) tracks
union term cost), an automatic switch to the whole decorrelated build (its
crossover is measured here), and changes to join streaming.
