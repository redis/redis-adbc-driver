# Deterministic collector takeover CI

Date: November 14, 2026 (America/Los_Angeles). Requestor: repository user.
Author/reviewer: Codex. Source: 0b21647831d13b0c10e652b7095515b0f5a74069.
Status: implemented, focused validation passed; human review pending. PR pending.

Release run 43564885518 failed its macOS Test step in TestSQLRekeyTakeoverWait:
600 expected directory members had become344 before the controlled collector's
barrier. The fixture connection's automatic collector can legitimately remove
one256-row batch first, invalidating the test's scheduling assumption.

[Specification](spec.md), [plan](plan.md), [validation](validation.md).
