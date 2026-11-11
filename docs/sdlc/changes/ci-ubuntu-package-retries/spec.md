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

Cold scanner builds patch only the expected pinned upstream overlay, replacing
its mutable release-tag reference and stale checksum with official release commit
`a3f81c6feb1133ba80600ac32e6d7c0b0b407e18` and its fixed archive SHA512. Unknown
overlay contents fail before modification. Include the pin script in binary-cache
identity. The current tag and commit archive contents were compared: all 1,750
files are byte-identical. SHA512 verification remains mandatory in vcpkg.
