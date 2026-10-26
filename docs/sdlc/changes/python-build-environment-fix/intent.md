# Repair Python CI environments after the dependency refresh

Date: 2026-10-22. Requester: Albert Wong. Author: Codex.
Source: `aab9612c7f08f92568b35175e48ed14867b67343`.
Status: implemented for review; maintainer approval pending.
See [specification](spec.md) and [plan and evidence](plan.md).

Merged [PR #440](https://github.com/alberttwong/redis-adbc-driver/pull/440)
left the validation project's hidden Python pin at 3.13 while its manifest
requires >=3.14. The release wheel environment also used unconstrained cached
backend tools, allowing wheel 0.47.0 despite the new >=0.48.0 requirement.
Fix those environment selections without changing library versions, SQL behavior,
validation assertions, release targets or the isolated Superset environment.
