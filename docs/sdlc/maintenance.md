# Maintenance, diagnosis and feedback

Status: proposed manual process, extracted 2026-10-20 from `6f42461`.
There is no production monitor, automatic triage queue, scheduled scan or
autonomous remediation configured by these documents.

## Start with reproducible evidence

For a report, record driver revision/release, Redis/Search versions, topology,
SQL with safe fixture data, Arrow schema, options, expected/actual results and
SQLSTATE. Include whether failure occurs at connect, prepare, execute, fetch or
close. Separate server/network/fixture failures from compatibility differences.

For a long-running query, the optional statement setting
`adbc.redis.execution_diagnostics_file` writes atomic JSON snapshots during
execute. Read the [diagnostics guide](../execution-diagnostics.md) before
interpreting counters: worker seconds overlap, client wait is not server CPU,
and `execute_returned` precedes complete Arrow fetch. Use a distinct private path
per run. Files contain SQL literals/metadata and need redaction before sharing.

Worker incidents can include `adbc.redis.query_worker_tuning`, actual grants,
active/queued counts, runtime CPU limits and concurrency. Do not replay writes
or change application `GOMAXPROCS` to collect a measurement. Preserve stopped
trial diagnostics and incomplete-coverage status.

## Triage routes

| Evidence | Proposed next action | Required record |
|---|---|---|
| Wrong rows/types/SQLSTATE or lifecycle leak | Minimize fixture, add regression, propose fix | New change intent/spec/plan and observed failing case |
| Transport/fixture failure | Repair test/deployment prerequisite and rerun | Infrastructure evidence separate from dialect score |
| Latency/throughput change | Compare matched workloads and resources | Raw trials, complete fetch and independent output check |
| Ownership, ACL or sensitive-data exposure | Have the responsible maintainer assess impact and containment | Restricted evidence and explicit decision |
| Incomplete measurement | Preserve it and scope the missing work | Status remains inconclusive; no readiness claim |

## Incident record template

For an actual incident, create `docs/sdlc/incidents/<incident-id>.md`. No real
production incident was verified during this extraction. Use this structure:

```markdown
# Incident: <observed symptom>
Status: investigating | mitigated | resolved
Detected at: <timestamp/timezone>
Source revision/release: <commit/tag>
Reporter and responsible maintainer: <actual identities when known>

## Impact and affected versions
## Expected and observed behavior
## Reproduction, environment and evidence links
## Timeline and decisions (include actor and timestamp)
## Cause (distinguish confirmed facts from hypotheses)
## Mitigation and recovery verification
## Regression case and validation results
## Follow-up intent/spec/plan links
## Remaining risk and closure decision
```

After a fix, verify the original failure and relevant supported paths, link its
PR/release, and add a durable regression case. If the cause exposes a recurring
agent mistake, update the short guide/review rule with verified project facts.

## Optional automation backlog

Before scheduling diagnosis, choose an owner, comparable workload and metrics
source. Candidate signals here are CI failure counts, repeated SQL corpus
outcomes, or controlled benchmark regressions; the repository has no production
SLO baseline. Establish baseline quality before selecting thresholds. The
autotuning algorithm's internal guard values are not production alert bands.

A future deterministic trigger could attach evidence and open a draft intent
for triage. Scope its credentials and actions in a reviewed spec, test noise and
duplicate suppression, and retain dismissal reasons. Releasing binaries,
replacing assets and changing external databases need their own authorized
routes. Scheduling or enabling such a process is separate work.
