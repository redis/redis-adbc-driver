# Verification

2026-11-24. Prepared on an isolated branch from public main. No original query
archive, root README or unrelated workspace file was changed. This publication
ran no SQL, server/container operation, native build or full benchmark.

The fixed evidence JSON was extracted by an explicit safe field allowlist from
three retained historical query archives plus their recorded metadata/provenance.
It includes numeric timed samples, optional warm-ups, categorical outcomes,
reference-only dimensions, issue URLs, SQL/archive/artifact checksums and selected
safe environment fields (including effective active 4× QPF). No operational raw
metadata/logs, runtime identifiers, endpoints, credentials or licenses are copied.

The new verifier recomputes every per-query median/ratio and checks all 134
expected query table rows, including 89 unstarted DS rows, plus summary counts,
matched totals and geometric means. `summarize_baseline.py --check` passed; it
reads only fixed public input and never modifies archives/discovers retries.

Verified historical counts: SSB 13 validated/13; TPC-H 12 validated/22 with 6
timeouts and 4 process failures; TPC-DS 1 validated/99, 7 timeouts, 1 process
failure, 1 interrupted attempt and 89 unstarted. These remain distinct from eight
selected validated focused DS cases across multiple artifacts. The original
shared-worker memory caveat is retained; OOMs are not isolated driver proofs.

All added Markdown local targets and explicit evidence privacy checks passed.
README diff is limited to a contents entry and the performance section. The
focused document preserves its per-candidate source/library identities and
warm-up/deadline/provenance caveats. Candidate 11 Q4 timeout is recorded separately. Candidate 12 Q4 completed and
validated eight rows/four columns; its warm-up, all three samples, median/ratio,
source/library identities and separate 16-CPU allocation are recorded without
relabeling earlier results. The selected native12 sample JSON was independently
read and its median/ratio recomputed. This is not an all-eight single-build or
99-query suite result.
