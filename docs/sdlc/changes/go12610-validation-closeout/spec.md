# Specification

Date: 2026-10-22. Status: implemented as a scoped validation record.
See the [intent](intent.md) and [plan](plan.md).

- Record the source commit, embedded Go 1.26.10 version, and six rebuilt
  library/CLI artifact hashes.
- State that the separate native fixture passed 18 measurements and nine
  independent comparisons.
- Apply the user's full-scale waiver only to this rebuild. Keep the benchmark's
  normal 18-measurement/nine-pair acceptance rule unchanged.
- Report the stopped full-scale run as four completed executions, two valid
  pairs, one cancelled execution, and remaining work not run.
- Preserve the distinction between completed tests, historical failure recovery,
  evidence preservation, and independently verified resource cleanup.
- Keep private operational scripts, account/bucket/resource identifiers, private
  locations, credentials, raw datasets, and raw Arrow outputs out of this PR.
- Do not close the broader API benchmark issue or claim public package release.
