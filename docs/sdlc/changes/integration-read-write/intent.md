# Intent: bidirectional integration examples

Date: 2026-11-01. Source: public `68b44d0`. Requestor: repository user in this
Codex chat. Author: Codex. Status: implemented and locally validated; human
review pending. [Spec](spec.md), [plan](plan.md), [validation](validation.md).

## Problem and consumers

The ecosystem table marks many ADBC paths green while demonstrating reads only.
Users need runnable examples of persisting their tool's computed output and a
harness that distinguishes those writes from test-fixture setup.

## Outcomes and scope

Cover all 34 green local-ADBC rows, plus Spark's bridge and DuckDB, with both
reads and writes. Retain native write APIs where available; add explicit custom
Arrow output examples for models, arrays, visualizations and Python workflows.
Execute actual guide code in isolated dependency-compatible environments.

## Exclusions and unresolved decisions

No native distributed sink, driver transaction/consistency work, new model or
artifact backend, upstream endorsement, Flight SQL compatibility claim, or
production/remote deployment validation. Dask's worker-local sink and generic
Airflow provider defaults remain tracked feature gaps. No release requested.
