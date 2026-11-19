# Full ClickBench comparison on c8g.metal-48xl

Date: November 18, 2026. Requestor: user in the benchmark-refresh chat.
Status: complete with recorded query limits.

Run the complete 43-query ClickBench suite using released Redis ADBC v0.0.29 on
Redis Software RAM and AWS c8g.metal-48xl, then compare with published entries
using that same instance type. Update slide 23 with audited fresh Redis results
and clearly identified published comparator versions/dates.

The user explicitly chose matching published hardware over the default Flex
storage choice, and subsequently cancelled the separate Flex backup because Flex
had only been a workaround for c6a.4xlarge's 32-GiB RAM limit. The task-owned Flex
load was stopped and its host terminated. No full-scale Flex backup is claimed.
The earlier four-query/custom-NVMe comparison plan is superseded.

The capacity pilot showed the managed layout could not fit safely within
384 GiB, including compaction headroom. The user then explicitly approved the
supported unmanaged table mode on the same c8g hardware, with compact HASH
storage and explicit tuning labels. All 105 typed columns remain required;
managed snapshots are not disabled or claimed for the unmanaged fixture.

See [specification](spec.md) and [plan](plan.md).
