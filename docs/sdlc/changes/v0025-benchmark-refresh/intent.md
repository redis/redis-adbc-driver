# v0.0.25 benchmark refresh

Date: 2026-11-12. Requestor: repository owner. Status: complete for the final user-authorized scope.
Source: release v0.0.25, `a8de1c3cce12138ae4c37cdbff1e8ed8875b3bc4`.

Retest driver performance and update the existing ecosystem presentation. The
final scope excludes the 100-million-row ClickBench rerun, canceled explicitly
by the user on November 12. Keep its results historical. The separate requested
default for future full-scale runs is Flex stopgap (`adbc.redis.flex=true`),
subject to actual driver support and a verified compatible backup. No fresh
full-scale ingestion is authorized.

Preserve accurate historical server/version labels, independently validate full
Arrow results, and keep infrastructure and operational evidence private.

See [spec](spec.md) and [plan](plan.md).

See [completed validation and review](validation.md).
