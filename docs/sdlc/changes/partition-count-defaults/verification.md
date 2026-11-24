# Verification

2026-11-24. Source-only option change on a clean public-main branch. No SQL,
Redis/container operation, native shared-library build or live partition read ran.
Both REDIS_URI and REDIS_SNAPSHOT_URI are unset for Go checks. GOMAXPROCS=2,
GOMEMLIMIT=512MiB, -p 1 and -count=1 bound local verification resources.

Focused option/incremental race tests passed (1.895s). They cover public wrapped
ADBC database initialization at unset/1/16/1024 values, string and int getters/
setters, invalid/overflow values preserving prior state, unknown typed-key
base delegation, connection shared-snapshot retention dispatch, actual
NewStatement copying, explicit statement override and unchanged existing
incremental state after parent changes. Existing statement-only closed-state
and incremental-option tests remain included.

Database Open's one-field scalar copy is reviewed statically; these pure tests
do not open a real Redis connection. A metadata-only database constructor and a
synthetic connection exercise option lifetimes without a server. Actual sixteen
returned/read partitions will require the separately authorized scanner/live gate.
No assertion of supported full-query partition parallelism is made.

All endpoint-free unit packages passed (root package 29.387s). Formatting, diff
and local SDLC reference checks pass. No live sixteen-token/read claim is made.
