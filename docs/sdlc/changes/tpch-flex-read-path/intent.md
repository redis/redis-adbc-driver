# Intent

Requested 2026-11-24 by the user: "fix the Flex read path so Q3/5/7/8/9 pass". Base: `fdd49f7c` (main with the snowflake joins of #635), rebased on `12f46745` (#640). Status: read-path changes implemented; the 60-second SF1 Flex goal is not met on the measured deployment (see [validation](validation.md#sf1-flex)). See [spec](spec.md), [plan](plan.md), [validation](validation.md).

On Redis Software Flex (`adbc.redis.flex=true`, 8 GiB RAM cache, verified 4x QPF), SF1 TPC-H Q3/Q5/Q7/Q8/Q9 exceed the 60-second statement deadline with the #635 build, although they validate within seconds on the RAM profile's 10% subset. Flex reads walk exact row-ID directories and fetch every HASH; Search supplies complete candidates only for filters matching at most 131,072 rows. The streamed facts already use the parallel exact-directory reader, but every multi-table dimension branch (orders⋈customer in Q3/Q5/Q7/Q8) was pre-joined by the eager join, whose scans read the whole orders directory serially. Per-row flash latency makes serial reads far slower on Flex than on RAM.

Make the large Flex reads of these queries parallel without changing results, NULL/duplicate semantics, snapshot visibility or the RAM-profile plans' correctness. Other Flex gaps (the disabled partitioned aggregate for Q1, the eager join for Q19, DuckDB-over-ADBC) are excluded unless a shared cause appears.
