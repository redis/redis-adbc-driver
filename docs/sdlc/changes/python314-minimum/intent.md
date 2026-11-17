# Require Python 3.14 or newer

Date: 2026-11-17. Source baseline: `d14eaf4e`. Author: Codex.
Requestor: repository user. Status: implemented and validated; review pending.

The user requested a Python 3.14 minimum and explicitly chose to mark Superset
6.1 unsupported while tracking its upstream dependency blocker. Remove the
separate older-Python support environments. Preserve dated historical evidence.
See [specification](spec.md) and [plan/validation](plan.md).
