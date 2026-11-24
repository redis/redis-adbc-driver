# Specification

Date: 2026-11-23. Base: `0bed7ce5`. Isolated prototype.
See [intent](intent.md), [plan](plan.md).

Admit only direct materialized memory sources without lazy/view/join/group or
source-key identity, outside retained/partition/sub-budget execution. Rebind
alias columns to real base-column names without mutating the original AST.
Before moving any conjunct, prove the ENTIRE residual consists of total typed
boolean logic/direct comparisons over ordinary integer, boolean, decimal or
string values. Refuse arithmetic/functions/casts, ambiguous coercions, floating
values, tagged/UUID aliases, collation-dependent types and unsupported expressions.
This prevents a later year condition from suppressing an earlier error/effect.

Preflight every referenced existing Value in metadata column order across all
rows before copying/consumption. Missing/defaulted fields, mismatched types or
malformed decimal storage refuse to the original copy path. From the proven
residual, bind only independently necessary integer/bool conjuncts using the
existing scanPredicate. Do not evaluate string/decimal clauses early. The normal
scan evaluates the unchanged complete original residual after qualification.

Allocate qualified maps only for necessary matches; preserve all copied fields,
NULLs, duplicate bag rows and hidden values. Original shared maps/metadata remain
unmodified, and scanJoinItem's normal cleanup still clears only its private view.
No source/default decode, external I/O, native proof, lifetime or budget change.
Cancel in validation/copy loops; never expose partial copies on cancellation.
