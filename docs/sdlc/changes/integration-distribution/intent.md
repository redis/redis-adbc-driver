# Intent

Date: 2026-11-01. Requestor: Albert Wong. Status: implemented; CI pending.
[Specification](spec.md), [plan](plan.md), [validation](validation.md).

Choose an installation route for every integration in the current 50-tool
inventory and automate distribution of this repository's drivers on future
releases. Reuse the managed installer, dbc packages and existing integration
helpers on current `redis/redis-adbc-driver` main. Python integrations share the
native wheel; R/native clients use the managed library; separate connectors,
framework custom code and upstream registry onboarding retain their own scope.
