# Plan

1. Reproduce SQL bypassing an admitted same-table writer on verified 4x QPF Software.
2. Share leased admission for SQL DML and Arrow, preserving unmanaged bypass.
3. Exercise concurrent/mixed writers, fresh-state evaluation, cancellation, lost
   replies, leases, independent targets and existing snapshot conflicts.
4. Run focused race and appropriate broader suites; benchmark four SQL writers
   without retries, validate full output, document limits and create a PR.

See [intent](intent.md), [spec](spec.md).
