# Intent: latest Redis Software default and Flex retest

Date: 2026-11-04. Base: `efb9cbf`. Requestor: user in this Codex chat.
Author: Codex. Status: proposed for review, not released.
[Spec](spec.md), [plan and evidence](plan.md), [issue #314](https://github.com/redis/redis-adbc-driver/issues/314).

Use the latest published Redis Software image for local and CI fixtures and
recheck Flex capabilities. Keep the current TLS, authentication, topology,
and verified 4x QPF requirements. Preserve dated historical measurements.
Implementing Flex support or fixing its connection guard is outside this PR.
