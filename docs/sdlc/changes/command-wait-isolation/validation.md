# Validation and decision

Source `dc6cceb5` was clean/frozen before all execution. All seven predeclared dense
jobs completed all 20K target BIGINT/payload value checks and unrelated COUNT/MIN/MAX
20000/1/20000. Each retained exactly 317 valid normal releases (314 copy, one index,
two install), all 2,219 observations. Requested probe connection relations and the
no-I/O pacing control passed, with one release Read/Write and zero pool misses/timeouts.
Unit and race each passed 14 cases, zero skips/failures. All 24 controller subprocesses
completed exit zero, including per-job resets and effective configuration gates.

Results support preceding elapsed-time/work sensitivity, not a production speedup.
Pure 20 ms pacing lowered release time but increased whole-job elapsed to 9.214 s,
versus 7.001/7.169 s controls. No server CPU/queue/TCP cause is established. Keep
production/default tests unchanged; exact diagnostics are archived unapplied.

Parent reviewed initial `0408724` source and `dc6` timer extension before timing;
no blockers. Agent review is not human approval. All loans ended with clients closed,
empty/index-free and effective 4x QPF / strict TLS / RESP3 verified; the same four-CPU /
eight-GiB container/image was observed at loan boundaries. Parent final teardown
removed owned infrastructure and generated TLS/environment/settings/fixture secrets;
the prior container and 31 networks were preserved. The report links sanitized
cleanup proof. Exact generated-credential scans found zero matches, leaving raw
measurement hashes unchanged.

See [report](../../../../benchmarks/table-modes/command-wait-2026-11-13/report.md),
[intent](intent.md), [contract](spec.md) and [plan](plan.md). Parent final artifact reviews at `6366fbd3` and `80da7b28` found no blockers,
including full audit, bounded conclusions and default-Go identity. These remain
agent reviews; no merge or human approval is claimed.

Read-only seven-job/2,219-observation audit, Python syntax, relative file links,
scoped path/URI and exact generated-credential scans passed (zero matches).
The exact diagnostic patch applies to baseline with git apply --check. Scoped
whitespace checks exclude archived patch context; original bytes/hash remain
preserved. No production Go/module/default test change is present.

A later parent-authorized capability-only follow-up found existing NET_RAW and
SO_TIMESTAMPNS option support inside an owned-container root process. A temporary
protocol-zero socket opened/closed without receiving frames. Standard packet tools
were absent, and reliable private release/flow, wall-clock and TCP/TLS correlation
was not established within minimal scope. Parent approved stopping before capture:
no workload rerun, packets, clock calibration or wire attribution. This is a
correlation/tooling scope constraint, not a permission blocker. The original seven
jobs and exact candidate patch remain unchanged; sanitized facts are linked in
the report and raw inventories remain private.
