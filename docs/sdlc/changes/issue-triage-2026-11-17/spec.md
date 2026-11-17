# Triage contract

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Source: `30a9eefbf13d19b3e3129171f8ecdc5579b852a2`. Status: verification complete; delivery recorded on the PR.

Read each open issue and all issue comments; compare closure candidates with
merged code, current documentation and explicit scope decisions. Record every
issue once in the disposition table. Close completed scope only with a linked
explanation; outstanding acceptance must remain visible.

Verify upstream contribution state/draft/merge fields from GitHub. Distinguish
merged external listings from generic integration endorsement and closed PRs
from merges. Preserve historical test settings and submission policies.

Verify materialized-view reader/lifecycle/reclamation regressions with race
detection and current Ray worker integration on a dedicated TLS/RESP3 Redis
Software fixture started with `make -C go redis-up`, active/effective 4x QPF,
noeviction and three primary shards. Record all failures and skips. Keep raw
logs, credentials and infrastructure identifiers private. No ClickBench run.

See [intent](intent.md), [plan](plan.md), [validation](validation.md).
