# Redis ADBC development records

Extracted on **2026-10-20** from local commit
`6f42461` (`docs: qualify Redis Kafka and Flink connectors as solutions`). The
working tree was clean before this documentation task. No live CI, Cloud
deployment or branch-protection state was queried, and historical tests were
not rerun to produce this baseline.

## Purpose and provenance

This directory applies the artifact handoffs described in Anthropic's
[AI-native SDLC playbook](https://claude.com/resources/articles/the-ai-native-sdlc-playbook):
intent, specification, implementation plan, verification, review, release and
maintenance records. This is a repository-specific extraction, not a copy of the
article or evidence that the project's earlier work used that process.

The code, Makefile, workflows and existing reports establish current behavior
and historical evidence. New workflow guidance is proposed. Human owners,
acceptance decisions and enforcement settings remain unassigned/unverified.

## Reading map

| Record | Purpose | Status |
|---|---|---|
| [Project context](context.md) | Source map, architecture and dated measurements | Extracted baseline |
| [intent.md](intent.md) | Project problem, users, outcomes and scope | Retrospective draft |
| [spec.md](spec.md) | Implemented contracts, constraints and acceptance evidence | Retrospective draft |
| [plan.md](plan.md) | Documentation deliverables and remaining adoption work | Documentation delivered; adoption proposed |
| [CLAUDE.md](../../CLAUDE.md) | Short session-start guide and executable commands | New advisory guidance |
| [Validation](validation.md) | Change-to-check mapping and evidence format | Existing checks; proposed reporting convention |
| [REVIEW.md](../../REVIEW.md) | Review passes and finding criteria | Proposed policy |
| [Release](release.md) | Existing packaging path, release evidence and recovery limits | Extracted runbook; proposed checklist |
| [Maintenance](maintenance.md) | Diagnosis and follow-up records | Proposed manual process |

## Use for the next change

Create `docs/sdlc/changes/<change-id>/` with its own `intent.md`, `spec.md` and
`plan.md`. Use the headings in the baseline files, but describe only that change.
Link those three files to one another and to its issue/PR. Include date, source
commit, status, actual author/requestor when known, and unresolved decisions.
Do not infer an approval from a generated document.

1. Record the problem, affected consumers, desired behavior and exclusions.
2. Describe the contract, implementation boundaries, risks and measurable checks.
3. List files, ordered work and verification; update the plan when scope changes.
4. Attach actual check results and a review decision to the PR.
5. If released, link tag, checksums and workflow run; if a regression follows,
   preserve its evidence and create a new intent linked to the incident.

For this proposed workflow, git is the source of truth for Markdown/code, PRs
for review decisions, workflow runs for checks, and release tags/assets for
distributions. Any external tracker should link those records rather than carry
an independently edited specification. No external tracker was inspected here.

## Adoption gaps and useful measurements

Checked-in PR tests already cover eight Redis/architecture combinations.
Postgres differential and autotuning readiness runs are manually dispatched.
There is no checked-in agent-evaluation workflow, artifact transition job,
production anomaly trigger, or hook in `.claude/settings.json`; that file only
enables the Redis development plugin. Required checks, release approvers and
production monitoring cannot be determined from the local checkout.

Once records exist, measure intent-to-spec time, plan-to-merge time, post-build
spec rework, first-pass CI success, important review findings, release regressions
and time to capture a regression case. Collect timestamps from commits, PRs and
workflow runs. No baseline for those process metrics is available here.
