# Redis Software 4x QPF development default

Date: 2026-10-22. Requestor: Albert Wong. Status: verified locally; PR review pending.

Albert requires Redis Software with active 4x QPF as this project's default development and test environment. Every Software fixture, including TLS and Flex refusal fixtures, must retain this setting. ClickBench requires the same distribution and QPF, with verified compatible backup restoration for repeat query runs. Valkey, AWS ElastiCache, and GCP Memorystore are outside support and compatibility testing.

This change updates fixture setup and standing instructions. It does not change driver query behavior, rewrite historical results, remove separately documented Redis OSS compatibility checks, publish private benchmark infrastructure, or claim that the corrected full-scale Software benchmark has run.

See [specification](spec.md) and [plan](plan.md).
