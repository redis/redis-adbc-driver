# Stale issue and PR status references

Date: 2026-11-24. Source: `0cde7e38` (main after PR #666). Requestor: Albert
Wong. Author: Claude Code. Status: implemented; human review pending.
Issue: [#657](https://github.com/redis/redis-adbc-driver/issues/657).
[Specification](spec.md), [plan](plan.md), [validation](validation.md).

Several pages still describe closed issues as open or blocking, and several call
upstream reports unsubmitted when they were submitted. Readers who follow those
links reach closed trackers, and the Superset CLI error still cites #471 as an
open blocker.

Desired outcome: every reference listed in #657 states the state GitHub reported
on the day of the change, names a current tracker when there is one, and says so
when there is none. The integration registry reports Superset as moved, like the
other moved adapters.

Out of scope: claiming Superset 7.x support (no run is recorded here), editing
the separate redis-adbc-integrations repository, rewriting historical change
records beyond the two dated notes #657 offers, and runtime behavior.
