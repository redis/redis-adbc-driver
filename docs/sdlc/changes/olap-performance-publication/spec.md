# Specification

Historical scope is fixed: SSB13 queries, TPC-H22 queries, and TPC-DS Q1–Q9
completed attempts with Q10 interrupted and Q11–Q99 not run on Redis. Preserve
original deployments, release checksums, timing samples, validation tolerances,
resource settings and shared-worker memory limitation. Only fully fetched and
validated three-sample pairs enter ratios. Failed case shapes belong to the
reference. No whole-suite score is inferred from a successful subset.

Publish only allowlisted numeric samples, statuses, source/artifact hashes,
public issue URLs and safe environment fields. Original operational archives
remain unchanged/private. Do not include endpoints, runtime/container/process
identifiers, credentials, licenses or raw logs. The statistics verifier reads
only the fixed sanitized JSON, never rewrites inputs and never discovers later
candidate directories. Existing runner semantics changed after the historical
run and cannot be described as an identical reproduction of it.

The focused DS follow-up is a separate document with per-candidate identities,
eight selected validated cases spanning multiple builds, Q4 passing only under
the separately labeled Candidate 12 profile, and no
full-suite/single-build pass claim. Targeted new Q4 outcomes are explicitly
labeled; historical numbers never get replaced. Scope README edits to the report
entry and section; preserve unrelated workspace changes by using a clean branch.
