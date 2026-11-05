# Implementation plan

Date: 2026-11-01. Source: public `68b44d0`. Status: complete locally; PR pending.
[Intent](intent.md), [spec](spec.md), [validation](validation.md).

## Ordered work

1. Inspect the current 50-tool comparison and existing guide execution harness.
2. Add the shared finite Arrow output sink and lifecycle regression checks.
3. Extend actual guide code and linked examples to persist computed outputs.
4. Require read/write evidence in the runner, and add core guide checks to CI.
5. Run all 36 applicable guides on a dedicated verified Software fixture,
   correct failures, and retain machine-readable results and limitations.
6. Update the comparison, guide index and shared setup; file issues and open PR.

## Review

Reviewed output-table ownership, ordinary-failure cleanup, independent result
verification, configured-endpoint handling, and claim scope. No native driver
behavior changed. Human maintainer review is pending; local review is not approval.
