# CI Ubuntu package reliability specification

Date: 2026-11-11. Author: Codex. Status: implemented for review.
See [intent](intent.md) and [plan](plan.md).

Both Ubuntu setup paths retry the update/install transaction up to three times,
with ten seconds between attempts. Updates use `APT::Update::Error-Mode=any`:
a partial index is a failure and cannot authorize installation. Apt retains two
acquisition retries and uses 30-second HTTP/HTTPS timeouts. Exhausted attempts
fail the job; persistent DNS outages remain failures. macOS and DNF paths are
unchanged. Cache statistics run only if ccache exists.

Acceptance: execute both actual shell loops with stubbed package commands;
verify immediate success, recovery from update/install failures, no install after
a failed update, and failure after three unsuccessful transactions. Redis suites
remain behind existing Software/QPF verification. No Redis tests are needed for
this shell-only change. Hosted CI is needed to validate real package downloads.
