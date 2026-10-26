# Bridge validation plan

Date: 2026-10-20. Author: Codex. Requestor: user in this chat.
Source commit: f5e90d8. Status: validation complete; datasource phase pending.
See [intent](intent.md), [spec](spec.md), and [verification](validation.md).

1. Build the current native library and install Ray Data into an isolated
   temporary Python environment.
2. Add optional-Ray regression tests for the exact proposed composition,
   Arrow schema/value fidelity, empty results and remote batch processing.
3. Execute against standalone Redis with Search and record actual results.
4. Keep the remaining #356 worker-side adapter work separate from this evidence.

The user subsequently requested a PR for this validation phase. Submit the
regression tests and evidence as a focused contribution referencing #356,
leaving the worker-side datasource and upstream acceptance work open.
