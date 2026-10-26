# Plan: establish repository development records

Date: 2026-10-20. Source: `6f42461`.
Status: documentation created by the current request; broader process adoption
is proposed and has no recorded maintainer approval.
Inputs: [intent](intent.md), [spec](spec.md), [context](context.md).

## Scope

Extract the implemented project and its evidence into usable Markdown records.
This plan does not reconstruct the order of historical implementation, choose
a new driver feature, or activate automation.

## Files and order of work

1. Inspect README, source boundaries, Makefile, workflows, validation quirks,
   differential coverage and benchmark reports. Capture commit and dates in
   [context.md](context.md).
2. Write [intent.md](intent.md) and [spec.md](spec.md), distinguishing inferred
   product goals from implemented behavior and unresolved decisions.
3. Add root [CLAUDE.md](../../CLAUDE.md) and [REVIEW.md](../../REVIEW.md) with
   verified paths, commands, correctness hazards and review criteria.
4. Document [validation](validation.md), [release](release.md) and
   [maintenance](maintenance.md); connect them through [README.md](README.md)
   and link the index from the project README.
5. Verify local links, command targets, numeric baselines, status language and
   whitespace. Report the documentation checks without implying driver tests ran.

## Verification for this documentation change

- Every relative file link resolves in this checkout; anchors are checked for
  the referenced section where present.
- Makefile targets and workflow names/triggers match their checked-in sources.
- Baseline numbers retain dates and source revisions; cancelled measurements
  remain cancelled. No approval, SLO or live deployment result is invented.
- Review the diff for unrelated edits and formatting errors.

Recorded checks for this extraction: all 83 local links/anchors across the ten
new Markdown files resolved, code fences were balanced, the source gate JSON
confirmed `cancelled`, and `git diff --check` passed. Driver build, Redis suites,
Cloud probes and benchmark runs were not executed for this Markdown-only change.

## Risks and alternatives

Duplicated command lists can drift: Makefile/workflows remain authoritative,
and the short guide links to detailed records. A baseline could look like an
approved feature: status blocks explicitly identify retrospective drafts.
Adding hooks or CI now would introduce permissions, dependencies and runtime
behavior beyond the documentation task; describe gaps and use a separate change.

## Proposed next adoption work

These items are not implemented by this documentation request. The maintainer
can scope each through its own linked intent/spec/plan:

1. Assign actual product, review and release decision owners and record where
   their approvals live; inspect branch settings and required checks.
2. Use `changes/<change-id>/` on one real driver change and collect complete
   acceptance/check/review evidence. Measure friction before automating it.
3. Build a bounded set of representative agent tasks with independently defined
   outcomes before proposing configuration-evaluation CI. Product regression
   tests alone do not evaluate agent behavior.
4. If needed, implement deterministic guards for specific known risks and test
   their behavior. State exact bypass/approval semantics in that change's spec.
5. Select a maintenance metric, establish a comparable baseline and owner, then
   propose trigger thresholds and permissions. No schedule or monitor is active.
