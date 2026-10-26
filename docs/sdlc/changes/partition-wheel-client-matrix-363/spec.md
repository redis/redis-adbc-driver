# Contract

Date: 2026-10-21. Source: 053e638 (merged #413). Requestor: Albert Wong (keep going); author: Codex. Status: in progress. Refs [#363](https://github.com/alberttwong/redis-adbc-driver/issues/363).

Test Python 3.13/3.14 with ADBC manager 1.8.0/1.12.0 on each native wheel platform (12 cells). Each cell installs the exact release artifact, requires packaged native-library loading without an override, and runs every existing verified TLS, independent ACL, exact Arrow, cross-process and lifecycle assertion. Preserve historical-ref behavior when checker helpers are absent. Remove owned fixtures on failure; upload only non-secret JSON. Release publication waits for this gate and downloads only package artifacts. Parent jobs keep full upstream suites; this matrix is partition-specific and does not establish Cloud coverage or every Python version supported by packaging. See [intent](intent.md) and [plan](plan.md).
