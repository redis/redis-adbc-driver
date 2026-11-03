# Intent: reconcile Flight SQL validation in the canonical inventory

Requested by the user on 2026-11-03: update the canonical Redis ADBC ecosystem
inventory with the new Flight SQL tool validations and open a PR. The Flight
repository removes its mirrored inventory and links here in Flight PR #58.

Status: documentation implemented; maintainer review pending.
Source base: bb83527f305b75ea731696f55f78eb75121bbbc0.

Update the 27 exercised Flight routes with operation-specific evidence, retain
historical environments and limitations, and reconcile Tableau/Metabase status
and totals. Do not change runtime behavior or imply new Software/Enterprise
qualification. [Specification](spec.md), [plan](plan.md), [validation](validation.md).
