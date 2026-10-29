# Polars regression rerun plan

Date: 2026-10-29. Author: Codex. Status: human review pending.

1. Build the native library and execute the linked suite unchanged alongside
   the driver tests; retain the original run's source and server provenance.
2. Repeat against upstream main using disposable Redis Software with verified
   4x QPF, as required by the current repository instructions.
3. Record results and reproduction in [validation](validation.md), update the
   Polars row in the support matrix, and check the documentation diff and links.
4. Submit a PR for human review; no driver or test implementation changes.

See [intent](intent.md) and [spec](spec.md).
