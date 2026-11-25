Target: redis/redis-adbc-driver — new issue
Title: Docs: refresh stale issue/PR status references (Superset #471, closed trackers, submitted upstream reports)
Labels: documentation
---
## Problem

Several docs describe closed issues as open, or say upstream reports were "not submitted" when they were. Checked 2026-11-24 at `682fae1f`.

### Superset (#471 closed COMPLETED 2026-11-17)

The only closing comment says Superset 7.x works. No Superset 7.x run is recorded in this repo, so the fixes below point to the moved guide instead of claiming support.

- `python/scripts/integration_registry.py:114`: the `UNSUPPORTED_GUIDES["superset"]` override still cites #471. Remove it so `superset` falls back to the generated "moved to" message; its URL is already in `MOVED_GUIDES` (`:109`).
- `scripts/test_integration_registry.py:79,82-92`: still asserts "unsupported", "Superset 6.1 is unsupported" and "issues/471". Update it with the registry change.
- These say "Superset 6.1 is (currently) unsupported…" and link #471. Point each to the moved Superset guide and note that #471 is closed:
  - `docs/integrations/superset.md:9`
  - `docs/integrations/acceptance.md:116`
  - `docs/integration-installation.md:55`
  - `docs/integrations/sqlalchemy.md:19`
  - `docs/python-integrations.md:173`, `:195`
  - `docs/setup.md:165`
  - `docs/driver-reference.md:5087` (keep the engine-spec description on that line)
  - `docs/data-engineering-adbc-support.md:3-4`
  - `docs/upstream-submissions/status.md:51-53`
  - `python/README.md:230-231`

### Closed issues described as open

- `benchmarks/olap-release/README.md:67`: "…tracked in #599". #599 closed on 10% subset results after #637; say the SF1 third-path rerun is still pending.
- `docs/redis-flex-stopgap.md:263-264`: "#329 remains excluded". #329 is closed; reword as "#329 (closed) was excluded".
- `docs/postgresql18-compatibility.md:17` and `docs/postgresql18-query-profile.md:10`: describe #118 as open. It is closed; list the open feature issues instead (#93, #98, #107, #110, #113, #114, #116, #360, #362, #363).
- `docs/postgresql18-query-profile.md:75`: "Five #361 cases remain open." #361 is closed, and `docs/sdlc/changes/postgresql18-final-query-gaps/validation.md:19` says all nineteen retained cases pass.
- `docs/partitioned-reads.md:408`, `:428`: remaining gaps "tracked in #170". #170 is closed; name a current tracker or say there is none.
- Optional, historical records: `benchmarks/olap-release/Q19-FOLLOWUP.md:29,43` (#599) and `docs/sdlc/changes/foreground-manifest-proof/validation.md:30` (#345, closed NOT_PLANNED). Add a dated note or leave them as history.

### Upstream reports that were submitted, or changed state

- `benchmarks/point-valid-isolation/selective-info-proposal.md:4`: "No upstream issue/comment has been submitted." It was: https://github.com/RediSearch/RediSearch/issues/12337 (open).
- `benchmarks/point-valid-isolation/upstream-draft.md:3`: "not submitted". It was: https://github.com/docker/desktop-feedback/issues/854 (open).
- `benchmarks/point-valid-isolation/README.md:205-207`: "unsubmitted upstream draft … No external issue/comment was made." Link both reports above.
- `docs/upstream-submissions/status.md:27`: https://github.com/Query-farm/adbc_scanner/pull/21 is listed as "Open; non-draft". It was closed without merge on 2026-11-24; the maintainer asked to retarget the v2.0 branch. Update the summary counts at `:9-10` and the check date at `:3` after re-checking all PRs.
- `docs/retained-partition-results.md:132-133`: add https://github.com/redis/go-redis/issues/4324 (open; go-redis does not bound declared bulk lengths) as the upstream context for the RESP admission guard. Optionally add it to the comment at `go/partition_retained_resp.go:25-27`.
- `docs/setup.md:229-230`: add https://github.com/apache/airflow/issues/76273 (open, needs-triage) as the upstream report for the generic AdbcHook limitation.

## How to verify

Run the docs build (`requirements-docs.txt`, `mkdocs.yml`) and the registry tests (`python -m pytest scripts/test_integration_registry.py`). Re-check each linked issue or PR state on the day of the change.
