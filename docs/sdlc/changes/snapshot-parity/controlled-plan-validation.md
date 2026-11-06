# Controlled cap and cursor-plan regression fixtures

Date: 2026-11-05. Status: focused verification complete; broad/platform gates
remain open. See [plan](plan.md), [hidden cap behavior](hidden-sort-cap-validation.md).

The ordinary owned Software fixture's aggregate result cap is **2147483648**;
its search-result cap is **1000000**. The latter value must not be mistaken for
the ordinary aggregate cap when restoring a native cap-137 fixture. A separate
coordinated run at aggregate cap 1000000 is nondefault evidence.

At the verified ordinary aggregate cap, the focused pre-fix selection fails
`TestSharedScanFallbacksAndPushdown`: its deliberately seeded cap 137 is treated
as undiscovered and replaced by the empty capability probe (case 0.14 s, root
1.999 s). The unrestricted/readiness cases in that selection pass. No returned
row mismatch was observed.

The test-only correction marks intentionally seeded cap values as known. A
validated, pinned physical cursor-leaf clone omits the logical baseline count
so the baseline admission path does not replace the algorithm under test.
The directory reference instead retains its complete logical descriptor and
expected row count. Published metadata and all production code are unchanged.
This preserves separate coverage of physical range/cursor mechanics and native
baseline/segmented visibility. The public unknown-cap tests continue to use
real discovery, including an actual server cap of 137.

The first expanded check caught the directory reference accidentally using the
physical clone's absent count; that fixture error was corrected before the
final run. The final ordinary Software `-race` selection passes **29 test
events**, no skips/failures/race reports, root **32.238 s**. It includes shared
scan results/fallbacks/lifetimes/counters, bounded readiness, exact rounded
sort/tie counters and parallel exact top-K. All existing row/order/projection/
data-aggregate prohibitions remain asserted. Synthetic measurement cases here
are correctness checks, not a new comparative performance or ClickBench claim.

The endpoint uses strict TLS, noeviction and API-verified active/effective 4x
QPF. Full ordinary, final combined snapshot, current hosted/platform and actual
Cluster API/recovery checks remain separate requirements.
