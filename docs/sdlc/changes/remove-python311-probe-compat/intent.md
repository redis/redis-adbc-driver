# Remove obsolete probe Python compatibility

Date: 2026-11-17. Source baseline: `d14eaf4e`. Requestor: repository user.
Status: implemented and validated; review pending. Author: Codex.

Remove the Python 3.11 archive-extraction workaround introduced in merged #467
and #469. Use the project's current Python 3.14 integration environment. This
supersedes the earlier records' older-Python compatibility requirements; recorded
historical runtime versions and data remain unchanged.

See [specification](spec.md) and [plan/validation](plan.md).
