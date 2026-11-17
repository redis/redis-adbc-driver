# Contract

Date: 2026-11-17. See [intent](intent.md) and [plan](plan.md).

Declare Requires-Python >=3.14 without an upper bound. The native installer
rejects older interpreters before parsing/installing; 3.14 and newer remain
accepted. Current integration profiles, workflow interpreters, acceptance
commands and example base images use 3.14. Go validation/pgdiff already require
3.14. No runtime/library or historical measurement is relabeled.

Superset 6.1 requires PyArrow <19, which has no usable CPython 3.14 wheels on the
checked Linux target. Remove its extra, supported profile and dedicated CI job.
Record it explicitly as unsupported in guide/selector metadata and current docs;
explicit requests fail with the tracker before executing a test or connecting.
Preserve the opt-in requalification probe and historical reports for future
upstream restoration. Track acceptance in project issue #471, linked to upstream
Superset SIP-234. Do not override upstream requirements or reinstate older Python.

Other dependency groups remain isolated. Resolution is not installation, live
integration qualification, native wheel validation or full upstream acceptance.
