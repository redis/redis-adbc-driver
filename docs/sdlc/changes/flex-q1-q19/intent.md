# Intent

Requested 2026-11-25 by the user: "fix Q1 and Q19 on Flex". Base: main `682fae1f`, which includes #643, #644 and #645. See [spec](spec.md), [plan](plan.md), [validation](validation.md).

#636 measured SF1 Flex Q1 at 95.8 s and Q19 at 804 s. Both read every lineitem row serially, with a worker grant of 0.
- **Q1:** `partitionedAggregate` returned early whenever Flex was enabled, so the exact grouped aggregate ran serially.
- **Q19:** the eager join (`lookup_join_order`) read lineitem through a single scan. Its Search filter matches about 214k rows, over the 131,072-row serial candidate budget, so the scan read the whole directory.

Make both reads parallel on Flex, without changing results, exact decimal sums, visibility or the complete predicate. RAM plans must be unchanged.

Expected limits, from [#643's measurements](../tpch-flex-read-path/validation.md#sf1-flex):
- **Q1** keeps about 98.6% of lineitem. On the 1-shard, 8 GiB RAM-tier deployment, cold flash reads bound a full read well above 60 s.
- **Q19's** lineitem candidates are a bounded set, so Q19 should fit.
