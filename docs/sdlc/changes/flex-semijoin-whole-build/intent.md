# Intent

Requested 2026-11-24: fix [#639](https://github.com/redis/redis-adbc-driver/issues/639). Base: `fdd49f7c` (main). Status: local fix; see [spec](spec.md), [plan](plan.md), [validation](validation.md).

On Software Flex, TPC-H SF1 Q22 through the direct driver times out at the 60 s deadline and reports "failed to validate statement snapshot". The lease check is only where the deadline expired. The cost is the decorrelated `NOT EXISTS (SELECT * FROM orders WHERE o_custkey = c_custkey)`.

The per-page semi-join probes from #630/#633 add a 256-key union that Search narrows on RAM. Flex snapshot reads traverse exact row-ID directories and ignore Search candidates, so every probe batch rescans the whole inner table. On the 10% subset, Q22 in Flex mode read 63.7M rows (about 425 passes over 150,000 orders) and timed out at 300 s; normal mode read 272,242 rows in 13.98 s.

Desired: a large Flex inner side is read once by the existing whole decorrelated build, with unchanged results and errors. Excluded: Flex read throughput in general (#636), RAM behavior, the timeout message wording.
