# Plan

Date: 2026-11-18. Requestor: Albert Wong. Source: 06302c39bd4cb5f8a4e0a1bfc978efc2d5d072ff. Status: implementation.

1. Extract only top-level durations from the last green CI run and retain slow-test placement hints.
2. Add Go inventory-based sharding with exact selection, failure propagation and sanitized reports.
3. Fan out ordinary/snapshot matrices while retaining verified fixture isolation; separate bridge and native validation.
4. Verify coverage/balancing/failure contracts, real Go discovery, workflow lint and documentation.
5. Open a PR and measure its complete hosted Test run against the baseline before claiming an observed improvement.

See [intent](intent.md), [spec](spec.md) and [validation](validation.md).
