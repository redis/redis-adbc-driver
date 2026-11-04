# Plan: installed-wheel C validation

Date: 2026-11-04. Author: Codex.
[Intent](intent.md), [spec](spec.md), [validation](validation.md).

- [x] Inspect all three failed release jobs; reproduce missing-header compilation.
- [x] Load the exact library through the public ADBC vtable.
- [x] Cover headerless renamed libraries and preserve safety assertions.
- [x] Trigger installed-wheel release checks for native validation changes.
- [x] Run local installed-wheel validation against verified Software 4x QPF.
- [ ] Verify replacement hosted Test, DuckDB and release checks.
