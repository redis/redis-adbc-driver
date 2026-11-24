# Intent

Date: 2026-11-23. Local base: frozen cohort prototype `defcd5a9`. Requested through the focused TPC-DS investigation. Status: separate local follow-up, unit-verified; no native build or live validation. See [specification](spec.md), [plan](plan.md), [validation](validation.md).

Remove repeated per-row string-map and name-search work from scalar cohort lanes by proving their column/predicate/AVG bindings once at registration. Keep the frozen published cohort production untouched until this separately reviewed follow-up is integrated. No SQL-specific columns, constants or benchmark changes.
