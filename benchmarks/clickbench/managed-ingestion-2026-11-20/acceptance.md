# Per-issue measurement acceptance

Date: 2026-11-20 local time. Evidence describes measured criteria, not invented
100M gates or a claim that an agent supplies human maintainer approval.

| Issue | Criterion | Evidence/status |
| --- | --- | --- |
| [#564](https://github.com/redis/redis-adbc-driver/issues/564) | Existing safety, eligibility and native fault/isolation behavior | Prior Software RAM/Flex QPF4 checks retained; see linked native validation. |
| #564 | Representative 105-column serial/four-writer unchanged/current comparison | Supported: 12 canonical mixed IPC controls and 12 streamed 262,144-row controls; independent all-value/schema/multiplicity validation. |
| #564 | Row-write/index population/proof/publication observations | Supported: positive transport hooks, bounded writes and 64→0 notification commands; durations are client command intervals, not server CPU. |
| #564 | CPU, RSS and I/O | Supported with stated limits: client process counters, direct data-Redis CPU, RSS/Arrow/GC traces and container I/O; proxy INFO CPU/per-PID I/O explicitly unavailable. |
| #565 | Isolated bounded compaction | Supported by nine validated 105-column one/two/four-batch isolated controls; see [concurrent closeout](issue-565.md). |
| #565 | Sustained concurrent four-writer backlog, phases and reclamation | Supported by six exact all-105-value repeated-wave cases: three waves ×four streams, n3 baseline/current; sampled copy/ingestion overlap, CPU/RSS/I/O, backlog and censored-normal/explicit-GC cleanup are separately reported. |
| #567 | Tractable all-105 values, reader transitions/types/lifetime and ownership | Supported by native/synthetic checks and exact 262,144-row controls. |
| #567 | Original large-row-group serial/four-reader memory plateau, separate Arrow/Go/cache accounting and safe policy tradeoffs | Supported at stated scopes: complete 99,997,497-row/226-original-group serial/four-reader source passes; n3 bounded warm-prefix old/new policy controls; independent native Go/Arrow/cache observations. See [reader closeout](issue-567.md). This is not full native 100M ingestion. |
| #566 | Managed load of exactly 99,997,497 rows and 105 canonical columns with independent validation | Not established. The user selected one writer for the eventual full-size load; tractable four-stream cases remain validated. Full native load is gated on defensible capacity/headroom; original source-reader controls do not count as ingestion. |
| #566 | Runtime phase/provenance and reusable backup readiness | Bounded phase/provenance observed; full-scale runtime and backup compatibility/readiness remain unverified. |

See [#564 closeout](issue-564.md), [method/hashes](method.json), and the
[product-version erratum](provenance-erratum.md). No AWS resources were started.
