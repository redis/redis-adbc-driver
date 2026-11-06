# Managed-copy inspection readiness

Status: isolated correction; not integrated into the stable repair head.

The generic HASH inspector still reports in-place adoption after managed import
became mandatory. Its checklist promises live application rows, destructive SQL
writes and obsolete DDL restrictions. A real Software regression reproduces
that report at base `03f5e4b` (root 0.935 seconds).

Inspection now reuses the managed adoption checklist with its existing bounded
sample. It makes no second source scan, initializes no catalog and modifies no
application data. Only qualifying row keys affect import type inference;
nested HASHes and copy's synthetic key column remain separate. Copy's original
types and membership retain their existing contract. Partial samples carry a
warning; a sample without import columns is blocked with an instruction to
inspect all source rows, rather than claiming that the entire source is empty.

The stable JSON/API names `sql_in_place` and `in_place_type` remain, with their
managed-import meaning documented. Text labels the column IMPORT TYPE. The
release smoke and CLI expectation require the managed-copy caption. Existing
copy/adopt checks and exact source preservation assertions remain enabled.

The focused report/refresh regression changes from failure to pass (root 2.112
seconds). It verifies separate copy/import types despite a nested invalid value,
source preservation, explicit refresh readiness and unchanged managed results.
The bounded partial-sample race check passes (root 1.826 seconds), with no Redis
client available to perform another scan or initialization.

The complete focused Software race selection passes **16 test events**, no
skips/failures: root **3.703 seconds**, CLI **2.427 seconds**, all six test-bearing
packages passing. It covers inspection, discovery, private copy names, actual
inspection before catalog creation, managed report/refresh/blockers, partial
samples and CLI check/copy/adopt. Strict TLS, noeviction and effective 4x QPF
are verified before testing. The first neighboring run exposed the remaining
legacy CLI caption assertion; updating that expectation preserves the checks.
Workflow YAML parses. No full platform, performance or broader parity claim is
inferred from these utility checks.

All no-endpoint Go unit packages pass afterward (root 26.796 seconds). Live
integration skips in that unit-only invocation are not compatibility evidence.
