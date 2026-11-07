# Utility staging audit and bounded metadata work

Date: 2026-11-06. Requestor: repository owner. Author: Codex. Base: `9c838db7a24d40f35ac503efc7861a0805a8abde`. Issue: [#341](https://github.com/redis/redis-adbc-driver/issues/341). Status: in progress.

Audit current public utilities before replacing historical limits. Managed utility row images already use bounded batches; eliminate unnecessary repeated capability metadata writes during streamed copies and prevent returned copy statistics from retaining input scans. Verify complete operations above the historical row/byte thresholds and accurately document remaining resource bounds. Preserve exact values, source ownership, one publication, cancellation and unknown-outcome handling.

See [spec](spec.md), [plan](plan.md), [validation](validation.md).
