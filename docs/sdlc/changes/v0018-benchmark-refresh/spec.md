# Specification: v0.0.18 benchmark publication

Date: 2026-10-29. Status: implemented for PR review. See [intent](intent.md) and
[plan](plan.md).

## Functional requirements and implementation

Publish all seven completed local suites and the visible medians/ranges. Retain
trial values sufficient to audit coverage, arithmetic and predeclared policy
gates. Label v0.0.18 source/build conditions and skipped full-scale scope.
The bulk runner and four focused tests are the previously measured code.

## Data and execution design

The allowlisted evidence omits endpoint/deployment identifiers and operational
logs. The audit uses the existing IPC summarizer and policy gate evaluator,
then checks the supplied summary against the retained trials. It does not execute
Redis queries. Raw result-row validation happened during the original runs.

## Acceptance criteria

- All 28 bulk, 98 incremental, 36 UNION, 144 IPC, 90 QPF-throughput, 48 policy,
  and 72 product-comparison trials/operations are accounted for.
- Correctness checks passed; policy performance passes remain 10/12.
- Recomputed figures agree with the report and slides.
- Four bulk helper tests pass; published links and paths resolve.
- No operational manifests, credentials, licenses, binary/dataset artifacts or
  personal filesystem paths are added to Git.

## Risks and unresolved concerns

The workstation is shared, ranges are descriptive rather than confidence
intervals, and historical server/toolchain differences prevent attributing
changes solely to the version. Full-scale reruns remain deferred. Later-main
production behavior has not been retested by this documentation change.
