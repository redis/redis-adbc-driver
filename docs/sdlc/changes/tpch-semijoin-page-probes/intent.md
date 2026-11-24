# Intent

Requested 2026-11-24: fix the open TPC-H GitHub issues. This change addresses
[#580](https://github.com/redis/redis-adbc-driver/issues/580) (SF1 Q4 exceeds the
60 s deadline in "checking bounded scan index readiness"). Base:
`c27bb970d938d25b82e427dcfb2efe59fd40d7a9` (main). Status: local fix, SF1
verification pending. See [spec](spec.md), [plan](plan.md), [validation](validation.md).

Q4's `EXISTS (SELECT * FROM lineitem WHERE l_orderkey = o_orderkey AND
l_commitdate < l_receiptdate)` is decorrelated as a semi-join, but a large inner
side (6M lineitems) is built only after about `innerRows / rowsPerRun` per-row
runs (60,000 at SF1). Q4's ~57,000 filtered orders never reach that threshold,
so every order runs its own subquery. Each run pays a full scan setup (bounded
range readiness via `FT.INFO`, planning, cursor). On a 10% subset, Q4 timed out at
120 s after reading 1.39 GB in 364,000 commands for 24,815 outer rows.

Desired: outer rows that are scanned in pages read the matching inner groups for
the whole page in a few bounded batches, never per row and never the entire inner
side, with unchanged results, NULL/three-valued semantics and error behavior.
Excluded: correlation other than top-level equalities (Q21's `<>`, tracked
separately), row-valued IN, join residuals evaluated outside `scanIter.keep`.
