# Plan

1. Add a combined lease/fresh-pin control operation and retain legacy behavior.
2. Route ordinary resumed worker steps through it; retain every existing metadata
   and source proof and release the read pin before row-copy work.
3. Regress separate-acquire round trips; test unknown replies/cleanup failures,
   owner refusal, reader pressure, corrupt metadata and changed source identity.
4. During parent-granted serialized slots, run focused unit/race and owned
   Software checks with verified active/effective 4x QPF and strict TLS/RESP3.
5. Compare pinned baseline/candidate native managed Arrow 1K-call workloads with
   one/four writers, plus compaction attribution/sustained controls. Fully fetch
   and independently validate values; preserve every attempt and actual limits.
6. Record disposition, scoped resource cleanup and parent review. No external
   publication by this subagent. See [contract](spec.md).
