# Specification

Date: 2026-11-18. Requestor: Albert Wong. Source: cacae16c637bd5459a43f9e794e8c80f7fafa08f. Status: implementation.

Destination repositories pin an immutable public driver revision as a submodule. DuckDB retains source, SQLLogicTests, runtime checks, pinned engine/toolchain and independent release artifacts. Framework adapters become a separate distribution and namespace, with isolated framework environments and preserved read/write and TLS lifecycle checks. Superset remains unsupported on Python 3.14+. Core wheel validation stops requiring extracted modules; no driver runtime or SQL behavior changes. Historical evidence remains dated and intact.

See [intent](intent.md) and [plan](plan.md).
