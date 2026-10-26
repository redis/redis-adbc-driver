# Evidence

2026-10-22. Requestor: Albert Wong. Author: Codex.

Base main: 590ab0a. git revert --no-commit applied without conflicts. The only
Go change removes the opt-in diagnostic test introduced by #499; production Go
code is unchanged. Verified relative Markdown links and git diff --check.
No Redis tests rerun for this documentation/test-removal change. EOF findings
remain in #498; no EOF fix or new server capability is claimed.

See [intent](intent.md), [spec](spec.md), [plan](plan.md), [evidence](evidence.md).
