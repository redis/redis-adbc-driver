# Bounded full-suite package budget

Date: 2026-11-04. Parent 02bd673. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The fresh 774-name ordinary continuation after the readiness fixture is terminal:
**879 test pass events, 249 explicit skips, no failed assertion events; package
failure at 600.729 s**. Go panicked at its default ten-minute package deadline.
TestSQLTagExactness had started only 0.719 s before the reported timeout, so
this run does not establish a TAG failure or a completed continuation. Its later
cases did not run. It also cannot be combined with the earlier failed full run
into a passing full suite.

Make's test/test-software targets now use a bounded 30-minute aggregate package
budget, overridable with GO_TEST_TIMEOUT. The hosted workflow's two Go test
commands use the same aggregate budget. Individual statement, transport,
cancellation, lease, publication and cleanup deadlines keep their existing bounds.
Existing assertions and explicit capability gates remain in place. The change
allows the expanded feature suite to reach later cases without disabling the
package deadline.

Make dry-run expansion confirms uncached sequential package execution with
-timeout=30m and requested race/JSON/failfast arguments. Inspected both workflow
commands and diff whitespace. This command/configuration inspection is not a
completed hosted run; a fresh complete ordinary pass under the longer package
budget remains required. No production/storage/ACL behavior change. Author review
is not independent maintainer approval.
